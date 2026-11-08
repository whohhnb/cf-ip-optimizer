package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"edgepilot/internal/agent"
	"edgepilot/internal/config"
	"edgepilot/internal/cryptox"
	"edgepilot/internal/dnsprovider"
	"edgepilot/internal/httpapi"
	"edgepilot/internal/logx"
	"edgepilot/internal/scanner"
	"edgepilot/internal/scheduler"
	"edgepilot/internal/service"
	"edgepilot/internal/store"
	"edgepilot/web"
)

func main() {
	log.SetFlags(log.LstdFlags)

	// 同一个二进制两种身份：默认是主控，`edgepilot probe` 是远端探针
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "probe":
			runProbe(os.Args[2:])
			return
		case "version", "-v", "--version":
			log.Printf("EdgePilot %s", httpapi.Version)
			return
		case "help", "-h", "--help":
			usage()
			return
		}
	}

	cfg := config.Load()

	dbPath := filepath.Join(cfg.DataDir, "edgepilot.db")
	st, err := store.Open(dbPath)
	if err != nil {
		logx.Fatalf("打开数据库失败: %v", err)
	}
	// every logx call now lands in stdout and in the in-app log table
	logx.SetLevelFromString(cfg.LogLevel)
	logx.SetSink(func(userID, domainID int64, level, msg string) {
		st.AddLog(userID, domainID, level, msg)
	})
	defer st.Close()

	// Webhook 默认只允许公网目标，避免用户把它当 SSRF 跳板打内网
	dnsprovider.SetAllowPrivateOutbound(st.GetSetting("allow_private_webhook", "false") == "true")

	box, err := cryptox.New(cfg.Secret)
	if err != nil {
		logx.Fatalf("初始化加密模块失败: %v", err)
	}

	hub := scanner.NewHub()
	svc := service.New(st, box, hub)
	srv := httpapi.New(cfg, st, svc, hub, web.FS())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc.ApplyRuntimeSettings()

	sched := scheduler.New(st, svc)
	go sched.Run(ctx)
	// 探针任务超时回收：探针崩了不能把域名槽位永久占住
	go svc.ProbeJanitor(ctx)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // SSE needs an unbounded write deadline
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		// 注入的可能是 1.0.0 也可能是 v1.0.0（make 传的是 git describe 结果），统一成一种
		ver := strings.TrimPrefix(httpapi.Version, "v")
		if ver != "dev" {
			ver = "v" + ver
		}
		logx.Infof(0, 0, "EdgePilot 边缘领航 %s · Cloudflare 优选 IP 调度", ver)
		logx.Infof(0, 0, "监听地址 %s · 数据目录 %s · 日志级别 %s", cfg.Addr, cfg.DataDir, cfg.LogLevel)
		if n, _ := st.CountUsers(); n == 0 {
			logx.Infof(0, 0, "首次启动: 请打开首页注册第一个账号（将自动成为管理员）")
		} else {
			logx.Infof(0, 0, "已加载 %d 个账号，调度器 10 秒后开始工作", n)
		}
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Fatalf("服务启动失败: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logx.Infof(0, 0, "收到退出信号，正在关闭服务…")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func usage() {
	log.Printf(`EdgePilot 边缘领航 %s

用法:
  edgepilot                      启动主控面板
  edgepilot probe [参数]          以探针身份运行（部署在电信/联通/移动的机器上）
  edgepilot version              打印版本

探针参数:
  --server   主控地址，如 https://panel.example.com
  --token    探针令牌，在面板「探针」页创建时显示一次
  --interval 拉取任务的间隔，默认 5s
  --once     只跑一轮就退出，便于排查连通性

示例:
  edgepilot probe --server https://panel.example.com --token ep_xxx
`, httpapi.Version)
}

func runProbe(args []string) {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	server := fs.String("server", "http://127.0.0.1:8080", "主控地址")
	token := fs.String("token", "", "探针令牌")
	interval := fs.Duration("interval", 5*time.Second, "拉取任务间隔")
	once := fs.Bool("once", false, "只跑一轮后退出")
	fs.Usage = usage
	_ = fs.Parse(args)

	agent.SetVersion(httpapi.Version)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := agent.Run(ctx, agent.Config{
		Server:   *server,
		Token:    *token,
		Interval: *interval,
		Version:  httpapi.Version,
		Once:     *once,
	}); err != nil {
		log.Fatalf("探针退出: %v", err)
	}
}
