package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"edgepilot/internal/agent"
	"edgepilot/internal/auth"
	"edgepilot/internal/config"
	"edgepilot/internal/cryptox"
	"edgepilot/internal/dnsprovider"
	"edgepilot/internal/httpapi"
	"edgepilot/internal/logx"
	"edgepilot/internal/model"
	"edgepilot/internal/scanner"
	"edgepilot/internal/scheduler"
	"edgepilot/internal/service"
	"edgepilot/internal/store"
	"edgepilot/web"
)

func main() {
	log.SetFlags(log.LstdFlags)

	// 全局 --data，用来在不设环境变量的情况下指向数据目录：
	// edgepilot --data /opt/edgepilot/data admin random
	os.Args = extractDataFlag(os.Args)

	// 同一个二进制两种身份：默认是主控，`edgepilot probe` 是远端探针
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "probe":
			runProbe(os.Args[2:])
			return
		case "admin":
			runAdmin(os.Args[2:])
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
		if cfg.EnvFile != "" {
			logx.Infof(0, 0, "已加载 %s（应用 %d 项，已存在的环境变量优先）", cfg.EnvFile, cfg.EnvFileVars)
		}
		if !cfg.TrustProxy {
			logx.Infof(0, 0, "未开启 PILOT_TRUST_PROXY：若面板在反向代理后面，日志里的客户端 IP 会记成代理地址")
		}
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
  edgepilot admin                查看管理员账号
  edgepilot admin random         随机生成一个新密码并设置
  edgepilot admin set <密码>      手动设置新密码
  edgepilot version              打印版本

全局参数:
  --data <目录>                  指定数据目录，等价于 PILOT_DATA_DIR

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

// extractDataFlag pulls "--data <dir>" / "--data=<dir>" out of the argument
// list and turns it into PILOT_DATA_DIR, so the recovery commands work without
// having to remember which environment the service was started with.
func extractDataFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--data" && i+1 < len(args):
			_ = os.Setenv("PILOT_DATA_DIR", args[i+1])
			i++
		case strings.HasPrefix(a, "--data="):
			_ = os.Setenv("PILOT_DATA_DIR", strings.TrimPrefix(a, "--data="))
		default:
			out = append(out, a)
		}
	}
	return out
}

// openStoreOffline opens the database without starting the server. SQLite runs
// in WAL mode, so this is safe while the service is up.
func openStoreOffline() *store.Store {
	cfg := config.Load()
	dbPath := filepath.Join(cfg.DataDir, "edgepilot.db")
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败（数据目录 %s）: %v", cfg.DataDir, err)
	}
	return st
}

// runAdmin mirrors OpenList/AList: no email needed, it always targets the
// administrator account, because the person running this is locked out.
func runAdmin(args []string) {
	fs := flag.NewFlagSet("admin", flag.ExitOnError)
	email := fs.String("email", "", "指定管理员账号（存在多个管理员时必填）")
	fs.Usage = usage
	_ = fs.Parse(args)
	rest := fs.Args()

	st := openStoreOffline()
	defer st.Close()

	all, err := st.ListUsers()
	if err != nil {
		log.Fatalf("读取账号失败: %v", err)
	}
	admins := make([]model.User, 0, 1)
	for _, u := range all {
		if u.Role == "admin" {
			admins = append(admins, u)
		}
	}
	if len(admins) == 0 {
		log.Fatal("还没有管理员账号，直接启动面板注册第一个即可")
	}

	var target *model.User
	if strings.TrimSpace(*email) != "" {
		for i := range admins {
			if strings.EqualFold(admins[i].Email, strings.TrimSpace(*email)) {
				target = &admins[i]
				break
			}
		}
		if target == nil {
			log.Fatalf("%s 不是管理员账号", *email)
		}
	} else {
		if len(admins) > 1 {
			fmt.Println("存在多个管理员，请用 --email 指定：")
			for _, u := range admins {
				fmt.Printf("  %s\n", u.Email)
			}
			log.Fatal("未指定 --email")
		}
		target = &admins[0]
	}

	if len(rest) == 0 {
		fmt.Printf("管理员账号：%s\n", target.Email)
		fmt.Println("用法：")
		fmt.Println("  edgepilot admin random        随机生成一个新密码")
		fmt.Println("  edgepilot admin set <密码>     手动设置新密码")
		fmt.Println("密码使用 bcrypt 单向哈希存储，无法查看原文，只能重置")
		return
	}

	var newPw string
	switch rest[0] {
	case "random":
		pw, err := randomPassword(16)
		if err != nil {
			log.Fatalf("生成密码失败: %v", err)
		}
		newPw = pw
	case "set":
		if len(rest) < 2 || strings.TrimSpace(rest[1]) == "" {
			log.Fatal("用法: edgepilot admin set <新密码>")
		}
		newPw = rest[1]
	default:
		log.Fatalf("未知子命令 %q，可用: random / set <密码>", rest[0])
	}
	if len(newPw) < 8 {
		log.Fatal("密码至少 8 位")
	}

	hash, err := auth.HashPassword(newPw)
	if err != nil {
		log.Fatalf("密码处理失败: %v", err)
	}
	if err := st.SetPassword(target.ID, hash); err != nil {
		log.Fatalf("保存失败: %v", err)
	}
	// 重置密码必须踢掉旧会话，否则已经登录的浏览器继续用着，重置等于没做
	ver, err := st.BumpTokenVersion(target.ID)
	if err != nil {
		log.Fatalf("保存失败: %v", err)
	}

	log.Printf("已重置 %s 的密码 · token_version=%d · 旧登录态已全部失效", target.Email, ver)
	if rest[0] == "random" {
		log.Printf("新密码：%s", newPw)
		log.Printf("登录后请立刻在「修改密码」里换掉")
	}
}

// randomPassword avoids ambiguous characters. Rejection sampling keeps the
// distribution uniform instead of leaning on a naive modulo.
func randomPassword(n int) (string, error) {
	const charset = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	limit := 256 - (256 % len(charset))
	out := make([]byte, 0, n)
	buf := make([]byte, n*2)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, charset[int(b)%len(charset)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}
