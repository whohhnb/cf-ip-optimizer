// Package service wires the scanner, the DNS providers and the store together.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"edgepilot/internal/cryptox"
	"edgepilot/internal/dnsprovider"
	"edgepilot/internal/logx"
	"edgepilot/internal/model"
	"edgepilot/internal/scanner"
	"edgepilot/internal/store"
)

type Service struct {
	Store *store.Store
	Box   *cryptox.Box
	Hub   *scanner.Hub

	mu      sync.Mutex
	running map[int64]bool

	// 全局扫描并发闸门。单次扫描默认 48 并发连接，若不设总量上限，
	// 一个用户连建十个域名就能把宿主的连接数和带宽吃满
	limMu     sync.Mutex
	limCond   *sync.Cond
	limActive int
	limMax    int
}

func New(st *store.Store, box *cryptox.Box, hub *scanner.Hub) *Service {
	s := &Service{Store: st, Box: box, Hub: hub, running: make(map[int64]bool), limMax: 3}
	s.limCond = sync.NewCond(&s.limMu)
	return s
}

// ApplyRuntimeSettings 把可热更新的设置应用到内存（启动与改设置时调用）
func (s *Service) ApplyRuntimeSettings() {
	s.SetMaxConcurrentScans(settingInt(s.Store, "scan_max_concurrent", 3))
}

// SetMaxConcurrentScans 调整全局扫描并发上限，调大后立刻唤醒排队中的任务
func (s *Service) SetMaxConcurrentScans(n int) {
	if n < 1 {
		n = 1
	}
	s.limMu.Lock()
	s.limMax = n
	s.limMu.Unlock()
	s.limCond.Broadcast()
}

func (s *Service) tryAcquireSlot() bool {
	s.limMu.Lock()
	defer s.limMu.Unlock()
	if s.limActive < s.limMax {
		s.limActive++
		return true
	}
	return false
}

func (s *Service) acquireSlot() {
	s.limMu.Lock()
	defer s.limMu.Unlock()
	for s.limActive >= s.limMax {
		s.limCond.Wait()
	}
	s.limActive++
}

func (s *Service) releaseSlot() {
	s.limMu.Lock()
	if s.limActive > 0 {
		s.limActive--
	}
	s.limMu.Unlock()
	s.limCond.Broadcast()
}

func (s *Service) IsRunning(domainID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[domainID]
}

// claim 原子地把域名标记为「扫描中」，已在跑则返回 false
func (s *Service) claim(domainID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[domainID] {
		return false
	}
	s.running[domainID] = true
	return true
}

func (s *Service) markRunning(domainID int64, v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v {
		s.running[domainID] = true
	} else {
		delete(s.running, domainID)
	}
}

func settingInt(st *store.Store, key string, def int) int {
	v := st.GetSetting(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func settingBool(st *store.Store, key string, def bool) bool {
	v := st.GetSetting(key, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// ScanOptions builds the scan configuration for one domain. Per-domain values
// win over the system defaults: the best PoP differs by carrier, so a single
// global whitelist would force everyone onto the intersection.
func (s *Service) ScanOptions(d *model.Domain) scanner.Options {
	colo := splitColo(d.ColoAllow)
	if len(colo) == 0 {
		colo = splitColo(s.Store.GetSetting("scan_colo_allow", ""))
	}
	return scanner.Options{
		Candidates:  settingInt(s.Store, "scan_candidates", 300),
		Concurrency: settingInt(s.Store, "scan_concurrency", 48),
		Samples:     settingInt(s.Store, "scan_samples", 3),
		Timeout:     time.Duration(settingInt(s.Store, "scan_timeout_ms", 1200)) * time.Millisecond,
		SpeedTop:    settingInt(s.Store, "scan_speed_top", 12),
		SpeedBytes:  settingInt(s.Store, "scan_speed_bytes", 3<<20),
		Port:        443,
		Host:        d.FullName(),

		LatencyMax:  float64(settingInt(s.Store, "scan_latency_max", 0)),
		SpeedFloor:  float64(settingInt(s.Store, "scan_speed_floor", 0)),
		SpeedOK:     settingInt(s.Store, "scan_speed_ok", 0),
		ColoAllow:   colo,
		NoSpeedTest: s.Store.GetSetting("scan_no_speed", "false") == "true",
	}
}

// splitColo turns "HKG, nrt ,SIN" into ["HKG","NRT","SIN"].
func splitColo(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		code := strings.ToUpper(strings.TrimSpace(part))
		if len(code) == 3 {
			out = append(out, code)
		}
	}
	return out
}

// StartScan launches a scan in the background and returns the job row at once.
func (s *Service) StartScan(ctx context.Context, d *model.Domain, trigger string) (*model.ScanJob, error) {
	// 先原子占位再建任务：分成「查在跑没」和「标记在跑」两步的话，
	// 手动扫描与调度器同时触发会各起一个 goroutine 扫同一个域名
	if !s.claim(d.ID) {
		return nil, errors.New("该域名正在扫描中")
	}
	opts := s.ScanOptions(d)
	// 上轮表现好的地址优先复测：anycast 下重新随机的边际收益极低，
	// 而一条已验证过的路径价值高得多
	seeds := opts.Candidates / 5
	if seeds < 8 {
		seeds = 8
	}
	if seeds > 40 {
		seeds = 40
	}
	opts.Seeds = s.Store.SeedIPs(d.ID, seeds)

	// 静态候选前缀：运维按线路配好的一份清单，只读。扫描不会回写它，
	// 测出来什么、选谁下发，仍然由当轮实测决定。
	opts.Prefixes = s.Store.CandidatePrefixes(model.LineKey(d.RecordLine))

	job, err := s.Store.CreateJob(d.ID, d.UserID, trigger, opts.Candidates)
	if err != nil {
		s.markRunning(d.ID, false)
		return nil, err
	}
	_ = s.Store.SetDomainStatus(d.ID, "scanning", "")

	// 绑了探针就走远端：本地视角代表不了电信/联通/移动的访客，
	// 悄悄退回本地扫描等于用错误的视角污染分线路排名
	if len(d.ProbeIDs) > 0 || d.ProbeID > 0 {
		if err := s.dispatchToProbes(ctx, d, job, opts); err != nil {
			_ = s.Store.FinishJob(job.ID, "failed", err.Error(), "", 0, 0, false)
			_ = s.Store.SetDomainStatus(d.ID, "error", err.Error())
			_ = s.Store.TouchDomainScan(d.ID)
			s.markRunning(d.ID, false)
			return nil, err
		}
		return job, nil // 槽位保持占用，直到探针回传结果
	}

	go func() {
		defer s.markRunning(d.ID, false)

		if !s.tryAcquireSlot() {
			// 全局名额已满：先告诉前端在排队，再阻塞等空位，
			// 不能直接拒绝——否则调度器一到点就报错，用户永远排不上
			_ = s.Store.UpdateJobProgress(job.ID, 0, 0, 0, "排队中")
			s.Hub.Publish(scanner.Event{
				Type: "progress", JobID: job.ID, DomainID: d.ID,
				Stage: "排队中", Msg: "前面还有扫描任务，排队等待空位",
			})
			s.acquireSlot()
		}
		defer s.releaseSlot()

		s.execute(job, *d, opts)
	}()
	return job, nil
}

func (s *Service) execute(job *model.ScanJob, d model.Domain, opts scanner.Options) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	name := d.FullName()
	started := time.Now()
	policy := "默认"
	if opts.NoSpeedTest {
		policy = "仅延迟"
	} else if opts.SpeedFloor > 0 {
		policy = fmt.Sprintf("速度≥%.0fMbps", opts.SpeedFloor)
	}
	if opts.LatencyMax > 0 {
		policy += fmt.Sprintf(" · 延迟≤%.0fms", opts.LatencyMax)
	}
	if len(opts.ColoAllow) > 0 {
		policy += " · 机房 " + strings.Join(opts.ColoAllow, "/")
	}
	logx.Infof(d.UserID, d.ID, "%s 扫描开始 · 触发=%s · 候选=%d（复用 IP %d + 前缀 %d）· 并发=%d · 超时=%s · 采样=%d次 · 策略 %s",
		name, triggerLabel(job.Trigger), opts.Candidates, len(opts.Seeds), len(opts.Prefixes),
		opts.Concurrency, opts.Timeout, opts.Samples, policy)

	var lastWrite time.Time
	lastStage := ""
	emit := func(p scanner.Progress) {
		progress := 0
		if p.Total > 0 {
			progress = p.Tested * 100 / p.Total
		}
		// log every stage transition so the console tells the whole story
		if p.Stage != lastStage {
			lastStage = p.Stage
			logx.Debugf(d.UserID, d.ID, "%s 进入阶段「%s」· %s", name, p.Stage, p.Msg)
		}
		if time.Since(lastWrite) < 900*time.Millisecond && p.Stage != "完成" {
			return
		}
		lastWrite = time.Now()
		_ = s.Store.UpdateJobProgress(job.ID, p.Tested, p.OK, progress, p.Stage)
		s.Hub.Publish(scanner.Event{
			Type: "progress", JobID: job.ID, DomainID: d.ID, Stage: p.Stage,
			Tested: p.Tested, Total: p.Total, OK: p.OK, Progress: progress, Msg: p.Msg,
		})
	}

	out, err := scanner.Run(ctx, opts, emit)
	elapsed := time.Since(started).Round(time.Millisecond)
	if err != nil {
		msg := err.Error()
		_ = s.Store.FinishJob(job.ID, "failed", msg, "", 0, 0, false)
		_ = s.Store.SetDomainStatus(d.ID, "error", msg)
		_ = s.Store.TouchDomainScan(d.ID)
		logx.Errorf(d.UserID, d.ID, "%s 扫描失败 · 耗时 %s · 测试 %d · 可用 %d · 原因: %s",
			name, elapsed, out.Tested, out.OK, msg)
		s.Hub.Publish(scanner.Event{Type: "error", JobID: job.ID, DomainID: d.ID, Error: msg})
		return
	}

	// persist the top slice only; the tail is not useful and bloats the DB
	keep := out.Results
	if len(keep) > 120 {
		keep = keep[:120]
	}
	_ = s.Store.InsertResults(job.ID, keep)

	var topIP string
	var topLat, topSpeed float64
	if len(out.Results) > 0 {
		topIP = out.Results[0].IP
		topLat = out.Results[0].Latency
		topSpeed = out.Results[0].Speed
	}

	logx.Infof(d.UserID, d.ID, "%s 扫描完成 · 耗时 %s · 测试 %d · 可用 %d · 最优 %s（%.0fms / %.1fMbps / 机房 %s）",
		name, elapsed, out.Tested, out.OK, topIP, topLat, topSpeed, coloOf(out.Results))

	pushed := false
	if d.AutoPush && len(out.Results) > 0 {
		if _, err := s.PushResults(context.Background(), &d, out.Results); err != nil {
			logx.Warnf(d.UserID, d.ID, "%s 自动下发失败 · %s", name, err.Error())
			_ = s.Store.SetDomainStatus(d.ID, "error", "下发失败: "+err.Error())
		} else {
			pushed = true
			_ = s.Store.SetDomainStatus(d.ID, "ok", "")
		}
	} else if len(out.Results) == 0 {
		logx.Warnf(d.UserID, d.ID, "%s 扫描结束但没有可用节点，未下发", name)
		_ = s.Store.SetDomainStatus(d.ID, "error", "没有可用节点")
	} else {
		_ = s.Store.SetDomainStatus(d.ID, "ok", "")
	}

	_ = s.Store.FinishJob(job.ID, "done", "", topIP, topLat, topSpeed, pushed)
	_ = s.Store.TouchDomainScan(d.ID)
	s.Hub.Publish(scanner.Event{
		Type: "done", JobID: job.ID, DomainID: d.ID, Stage: "完成",
		Tested: out.Tested, Total: out.Tested, OK: out.OK, Progress: 100,
		Msg: fmt.Sprintf("最优节点 %s · %.0fms", topIP, topLat),
	})
}

func triggerLabel(t string) string {
	switch t {
	case "manual":
		return "手动"
	case "create":
		return "新建域名"
	case "health":
		return "故障切换"
	default:
		return "定时"
	}
}

func coloOf(results []model.ScanResult) string {
	if len(results) == 0 || results[0].Colo == "" {
		return "-"
	}
	return results[0].Colo
}

// PushResults writes the best N addresses to DNS for a domain.
func (s *Service) PushResults(ctx context.Context, d *model.Domain, results []model.ScanResult) (int, error) {
	if len(results) == 0 {
		return 0, errors.New("没有可下发的优选 IP")
	}
	acct, encKey, err := s.Store.AccountSecret(d.AccountID)
	if err != nil {
		return 0, fmt.Errorf("读取 DNS 凭据失败: %w", err)
	}
	plain, err := s.Box.Open(encKey)
	if err != nil {
		if errors.Is(err, cryptox.ErrWrongKey) {
			return 0, errors.New("DNS 凭据解密失败：PILOT_SECRET 与保存凭据时不一致，请到「DNS 账号」重新填写一次凭据")
		}
		return 0, fmt.Errorf("解密 DNS 凭据失败: %w", err)
	}
	cred := dnsprovider.Credentials{}
	if plain != "" {
		if err := json.Unmarshal([]byte(plain), &cred); err != nil {
			return 0, fmt.Errorf("解析 DNS 凭据失败: %w", err)
		}
	}
	provider, err := dnsprovider.New(acct.Provider, cred)
	if err != nil {
		return 0, err
	}
	desc, _ := dnsprovider.DescriptorOf(acct.Provider)

	slots := d.Slots
	if slots <= 0 {
		slots = 2
	}
	if desc, ok := dnsprovider.DescriptorOf(acct.Provider); ok && desc.MaxSlots < slots {
		slots = desc.MaxSlots
	}
	desired := make([]string, 0, slots)
	seen := map[string]bool{}
	for _, r := range results {
		if len(desired) >= slots {
			break
		}
		if seen[r.IP] {
			continue
		}
		seen[r.IP] = true
		desired = append(desired, r.IP)
	}

	logx.Infof(d.UserID, d.ID, "%s 开始下发 · 服务商=%s · 目标 %s · TTL %ds · %s",
		d.FullName(), desc.Label, strings.Join(desired, ", "), d.TTL,
		map[bool]string{true: "滚动替换", false: "整批替换"}[settingBool(s.Store, "rolling_replace", true)])

	managed, err := s.Store.ListManaged(d.ID)
	if err != nil {
		return 0, err
	}
	res, err := dnsprovider.Sync(ctx, provider, dnsprovider.SyncOptions{
		RootDomain: d.RootDomain,
		SubDomain:  d.SubDomain,
		RecordLine: d.RecordLine,
		TTL:        d.TTL,
		Slots:      slots,
		Desired:    desired,
		Managed:    managed,
		Rolling:    settingBool(s.Store, "rolling_replace", true),
		Remark:     "edgepilot",
	})
	if err != nil {
		return 0, err
	}
	for _, m := range res.Managed {
		_ = s.Store.UpsertManaged(d.ID, m.Slot, m.RecordID, m.Value)
	}
	if len(res.Changes) > 0 {
		_ = s.Store.TouchDomainPush(d.ID)
		logx.Infof(d.UserID, d.ID, "%s 下发完成 · 变更 %d 项: %s",
			d.FullName(), len(res.Changes), strings.Join(res.Changes, "; "))
	} else {
		logx.Infof(d.UserID, d.ID, "%s 下发跳过 · 解析已是最新（%s）",
			d.FullName(), strings.Join(desired, ", "))
	}
	return len(desired), nil
}

// PushLatest re-pushes the newest completed scan without scanning again.
func (s *Service) PushLatest(ctx context.Context, d *model.Domain) (int, error) {
	job, err := s.Store.LatestDoneJob(d.ID)
	if err != nil {
		return 0, errors.New("还没有可用的扫描结果")
	}
	results, err := s.Store.ResultsByJob(job.ID, 10)
	if err != nil {
		return 0, err
	}
	return s.PushResults(ctx, d, results)
}

// TestAccount validates DNSPod credentials (optionally against a root domain).
func (s *Service) TestAccount(ctx context.Context, provider string, cred dnsprovider.Credentials, root string) error {
	p, err := dnsprovider.New(provider, cred)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return p.Verify(ctx, root)
}

// HealthWatch checks the currently published IPs and reports whether they are
// still reachable. It returns true when a rescan is warranted.
func (s *Service) HealthWatch(ctx context.Context, d *model.Domain) (bool, error) {
	managed, err := s.Store.ListManaged(d.ID)
	if err != nil || len(managed) == 0 {
		return false, err
	}
	alive := 0
	for _, m := range managed {
		if scanner.HealthCheck(ctx, m.Value, 443, 1500*time.Millisecond) {
			alive++
		}
	}
	if alive == 0 {
		return true, nil
	}
	return false, nil
}
