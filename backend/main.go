package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // SSE needs an unbounded write deadline
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		ver := httpapi.Version
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
