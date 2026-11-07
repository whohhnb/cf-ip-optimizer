package scheduler

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"edgepilot/internal/logx"
	"edgepilot/internal/model"
	"edgepilot/internal/service"
	"edgepilot/internal/store"
)

type Scheduler struct {
	svc *service.Service
	st  *store.Store

	mu        sync.Mutex
	lastCheck map[int64]time.Time
}

func New(st *store.Store, svc *service.Service) *Scheduler {
	return &Scheduler{svc: svc, st: st, lastCheck: make(map[int64]time.Time)}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// let the server finish booting before the first sweep
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}
	s.sweep(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

func (s *Scheduler) sweep(ctx context.Context) {
	domains, err := s.st.AllDomains()
	if err != nil {
		logx.Errorf(0, 0, "调度器读取域名失败 · %v", err)
		return
	}
	logx.Debugf(0, 0, "调度器巡检 · 共 %d 个域名", len(domains))
	for i := range domains {
		d := domains[i]
		if !d.Enabled || ctx.Err() != nil {
			continue
		}
		if s.svc.IsRunning(d.ID) {
			continue
		}

		interval := d.IntervalMin
		if interval < 5 {
			interval = 5
		}
		due := d.LastScanAt == nil || time.Since(*d.LastScanAt) >= time.Duration(interval)*time.Minute
		if due {
			logx.Infof(d.UserID, d.ID, "%s 到达扫描周期（每 %d 分钟），排队执行", d.FullName(), interval)
			go s.startScan(ctx, d, "schedule")
			continue
		}

		// failover watchdog: if every published IP is unreachable, rescan now
		s.mu.Lock()
		last := s.lastCheck[d.ID]
		s.mu.Unlock()
		if time.Since(last) < 3*time.Minute {
			continue
		}
		s.mu.Lock()
		s.lastCheck[d.ID] = time.Now()
		s.mu.Unlock()

		need, err := s.svc.HealthWatch(ctx, &d)
		if err != nil {
			continue
		}
		if need {
			logx.Warnf(d.UserID, d.ID, "%s 健康巡检：当前解析 IP 全部不可达，立即触发重扫", d.FullName())
			go s.startScan(ctx, d, "health")
		} else {
			logx.Debugf(d.UserID, d.ID, "%s 健康巡检通过", d.FullName())
		}
	}
}

func (s *Scheduler) startScan(ctx context.Context, d model.Domain, trigger string) {
	// small jitter keeps concurrent scans from hammering the network at once
	time.Sleep(time.Duration(rand.Intn(3000)) * time.Millisecond)
	if ctx.Err() != nil {
		return
	}
	if _, err := s.svc.StartScan(ctx, &d, trigger); err != nil {
		logx.Warnf(d.UserID, d.ID, "%s 启动扫描失败 · %v", d.FullName(), err)
	}
}
