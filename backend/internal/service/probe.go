package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"edgepilot/internal/logx"
	"edgepilot/internal/model"
	"edgepilot/internal/scanner"
)

// ProbeOptions is the wire format for a scan request. It mirrors
// scanner.Options but is spelled out with json tags so the agent and the
// controller can evolve independently.
type ProbeOptions struct {
	Candidates  int      `json:"candidates"`
	Concurrency int      `json:"concurrency"`
	Samples     int      `json:"samples"`
	TimeoutMs   int      `json:"timeoutMs"`
	SpeedTop    int      `json:"speedTop"`
	SpeedBytes  int      `json:"speedBytes"`
	Port        int      `json:"port"`
	IPv6        bool     `json:"ipv6"`
	LatencyMax  float64  `json:"latencyMax"`
	SpeedFloor  float64  `json:"speedFloor"`
	SpeedOK     int      `json:"speedOK"`
	ColoAllow   []string `json:"coloAllow"`
	NoSpeedTest bool     `json:"noSpeedTest"`
	Seeds       []string `json:"seeds"`
	// Fixed is the controller-generated candidate list. Every probe in one job
	// receives the same list, otherwise merging by IP is meaningless.
	Fixed []string `json:"fixed,omitempty"`
}

// ProbePayload is the body of a queued scan task.
type ProbePayload struct {
	Host    string       `json:"host"`
	Options ProbeOptions `json:"options"`
}

func toProbeOptions(o scanner.Options) ProbeOptions {
	return ProbeOptions{
		Candidates:  o.Candidates,
		Concurrency: o.Concurrency,
		Samples:     o.Samples,
		TimeoutMs:   int(o.Timeout / time.Millisecond),
		SpeedTop:    o.SpeedTop,
		SpeedBytes:  o.SpeedBytes,
		Port:        o.Port,
		IPv6:        o.IPv6,
		LatencyMax:  o.LatencyMax,
		SpeedFloor:  o.SpeedFloor,
		SpeedOK:     o.SpeedOK,
		ColoAllow:   o.ColoAllow,
		NoSpeedTest: o.NoSpeedTest,
		Seeds:       o.Seeds,
		Fixed:       o.Fixed,
	}
}

// Options converts the wire format back into scanner options on the agent side.
func (p ProbeOptions) Options(host string) scanner.Options {
	return scanner.Options{
		Candidates:  p.Candidates,
		Concurrency: p.Concurrency,
		Samples:     p.Samples,
		Timeout:     time.Duration(p.TimeoutMs) * time.Millisecond,
		SpeedTop:    p.SpeedTop,
		SpeedBytes:  p.SpeedBytes,
		Port:        p.Port,
		IPv6:        p.IPv6,
		Host:        host,
		LatencyMax:  p.LatencyMax,
		SpeedFloor:  p.SpeedFloor,
		SpeedOK:     p.SpeedOK,
		ColoAllow:   p.ColoAllow,
		NoSpeedTest: p.NoSpeedTest,
		Seeds:       p.Seeds,
		Fixed:       p.Fixed,
	}
}

// ProbeReport is what an agent posts back after running a scan.
type ProbeReport struct {
	Status    string             `json:"status"` // done | failed
	Error     string             `json:"error"`
	Tested    int                `json:"tested"`
	OK        int                `json:"ok"`
	ElapsedMs int64              `json:"elapsedMs"`
	Results   []model.ScanResult `json:"results"`
}

// effectiveConcurrency applies the per-probe cap. A probe value of 0 means
// "follow the global setting"; a larger value never raises the global limit,
// because that limit also protects the host's own bandwidth.
func effectiveConcurrency(global, probe int) int {
	if probe > 0 && probe < global {
		return probe
	}
	return global
}

// dispatchToProbes queues one scan task per bound agent.
//
// The candidate list is generated here, once, and shipped to every agent.
// Two independent random samples out of 1.5M addresses barely overlap, so
// letting each agent sample on its own would make the per-IP merge below
// meaningless.
func (s *Service) dispatchToProbes(ctx context.Context, d *model.Domain, job *model.ScanJob, opts scanner.Options) error {
	ids := d.ProbeIDs
	if len(ids) == 0 && d.ProbeID > 0 {
		ids = []int64{d.ProbeID}
	}
	if len(ids) == 0 {
		return errors.New("没有可用的探针")
	}

	probes := make([]*model.Probe, 0, len(ids))
	for _, id := range ids {
		p, err := s.Store.ProbeByID(id, d.UserID)
		if err != nil {
			return errors.New("绑定的探针不存在，请重新选择")
		}
		if !p.Online {
			seen := "从未上线"
			if p.LastSeenAt != nil {
				seen = "最后心跳 " + p.LastSeenAt.Format("01-02 15:04")
			}
			return fmt.Errorf("探针「%s」已离线（%s），请检查探针进程", p.Name, seen)
		}
		probes = append(probes, p)
	}

	// 统一候选列表。取不到地址段时降级为各探针自行采样，聚合退化成
	// 「能对上几个算几个」，但要明确告诉用户，不能装作正常
	if ips, err := scanner.PickCandidates(ctx, opts.Candidates, opts.IPv6); err != nil {
		logx.Warnf(d.UserID, d.ID, "%s 主控无法获取地址段，探针将各自采样：%v", d.FullName(), err)
	} else {
		opts.Fixed = ips
		opts.Candidates = len(ips)
		opts.Seeds = nil
	}

	for _, p := range probes {
		o := opts
		// 家宽的 NAT 表扛不住服务器的并发，按探针自己的上限收敛
		o.Concurrency = effectiveConcurrency(opts.Concurrency, p.Concurrency)

		body, err := json.Marshal(ProbePayload{Host: d.FullName(), Options: toProbeOptions(o)})
		if err != nil {
			return err
		}
		if _, err := s.Store.CreateProbeTask(p.ID, d.ID, job.ID, string(body)); err != nil {
			return fmt.Errorf("创建探针任务失败: %w", err)
		}
		logx.Infof(d.UserID, d.ID, "%s 派发探针 · %s（%s）· 候选=%d · 并发=%d",
			d.FullName(), p.Name, p.Line, o.Candidates, o.Concurrency)
	}

	_ = s.Store.UpdateJobProgress(job.ID, 0, 0, 0, "已派发")
	s.Hub.Publish(scanner.Event{
		Type: "progress", JobID: job.ID, DomainID: d.ID, Stage: "已派发",
		Msg: "已派发给 " + strconv.Itoa(len(probes)) + " 个探针",
	})
	return nil
}

// SubmitProbeResult stores one agent's raw output and, once every agent for the
// job has reported, merges and publishes the winner.
func (s *Service) SubmitProbeResult(ctx context.Context, probeID, taskID int64, rep ProbeReport) (int, error) {
	task, err := s.Store.ProbeTaskByID(taskID)
	if err != nil {
		return 0, errors.New("任务不存在")
	}
	if task.ProbeID != probeID {
		// a probe may only report on its own tasks
		return 0, errors.New("任务不属于该探针")
	}
	if task.Status == "done" || task.Status == "failed" {
		return 0, errors.New("任务已结束")
	}

	d, err := s.Store.DomainByIDInternal(task.DomainID)
	if err != nil {
		_ = s.Store.FinishProbeTask(task.ID, "failed", "域名已删除")
		_ = s.Store.FinishJob(task.JobID, "failed", "域名已删除", "", 0, 0, false)
		s.markRunning(task.DomainID, false)
		return 0, errors.New("域名已删除")
	}
	name := d.FullName()

	msg := strings.TrimSpace(rep.Error)
	results := sanitizeProbeResults(rep.Results)
	switch {
	case rep.Status == "failed" || msg != "":
		if msg == "" {
			msg = "探针执行失败"
		}
		_ = s.Store.FinishProbeTask(task.ID, "failed", msg)
		logx.Warnf(d.UserID, d.ID, "%s 探针执行失败 · %s", name, msg)
	case len(results) == 0:
		_ = s.Store.FinishProbeTask(task.ID, "failed", "探针没有返回可用节点")
		logx.Warnf(d.UserID, d.ID, "%s 探针返回 0 个可用节点", name)
	default:
		raw, err := json.Marshal(results)
		if err != nil {
			return 0, err
		}
		if err := s.Store.SaveProbeTaskResults(task.ID, string(raw), rep.Tested); err != nil {
			return 0, fmt.Errorf("保存结果失败: %w", err)
		}
		logx.Infof(d.UserID, d.ID, "%s 探针回传 · 测试 %d · 可用 %d · 耗时 %dms",
			name, rep.Tested, len(results), rep.ElapsedMs)
	}

	// 部分回传时给个进度，让界面知道还在等谁
	if total, done := s.taskProgress(task.JobID); total > 0 {
		_ = s.Store.UpdateJobProgress(task.JobID, 0, 0, done*100/total,
			fmt.Sprintf("探针回传 %d/%d", done, total))
		s.Hub.Publish(scanner.Event{
			Type: "progress", JobID: task.JobID, DomainID: d.ID,
			Stage: "等待探针", Tested: 0, Total: total, OK: done,
			Msg: fmt.Sprintf("已回传 %d / %d 个探针", done, total),
		})
	}

	return s.finishJobIfReady(ctx, task.JobID, d)
}

// taskProgress counts how many tasks of a job are finished.
func (s *Service) taskProgress(jobID int64) (total, done int) {
	tasks, err := s.Store.ProbeTasksByJob(jobID)
	if err != nil {
		return 0, 0
	}
	for _, t := range tasks {
		if t.Status == "done" {
			done++
		}
	}
	return len(tasks), done
}

// finishJobIfReady merges and publishes once every task of the job has settled.
// It is called from both the result path and the expiry janitor, so a crashed
// agent cannot leave the job hanging.
func (s *Service) finishJobIfReady(ctx context.Context, jobID int64, d *model.Domain) (int, error) {
	tasks, err := s.Store.ProbeTasksByJob(jobID)
	if err != nil {
		return 0, err
	}
	if len(tasks) == 0 {
		return 0, nil
	}
	for _, t := range tasks {
		if t.Status == "pending" || t.Status == "running" {
			return 0, nil // still waiting
		}
	}

	// 只有第一个到达的调用者能推进；其余在任务表已结算后直接返回
	job, err := s.Store.JobByID(jobID)
	if err != nil {
		return 0, err
	}
	if job.Status == "done" || job.Status == "failed" {
		return 0, nil
	}
	defer s.markRunning(d.ID, false)

	name := d.FullName()

	sets := make([][]model.ScanResult, 0, len(tasks))
	failures := 0
	testedTotal := 0
	var firstErr string
	for _, t := range tasks {
		if t.Tested > testedTotal {
			testedTotal = t.Tested
		}
		if t.Status != "done" || strings.TrimSpace(t.Results) == "" {
			failures++
			if firstErr == "" && t.Error != "" {
				firstErr = t.Error
			}
			continue
		}
		var rows []model.ScanResult
		if err := json.Unmarshal([]byte(t.Results), &rows); err != nil {
			failures++
			continue
		}
		sets = append(sets, rows)
	}

	if len(sets) == 0 {
		msg := firstErr
		if msg == "" {
			msg = "所有探针都没有返回可用节点"
		}
		_ = s.Store.FinishJob(jobID, "failed", msg, "", 0, 0, false)
		_ = s.Store.SetDomainStatus(d.ID, "error", msg)
		_ = s.Store.TouchDomainScan(d.ID)
		logx.Warnf(d.UserID, d.ID, "%s 全部探针失败 · %s", name, msg)
		s.Hub.Publish(scanner.Event{Type: "error", JobID: jobID, DomainID: d.ID, Error: msg})
		return 0, nil
	}

	merged := mergeProbeResults(sets)
	if len(merged) == 0 {
		msg := "探针结果合并后没有可用节点"
		_ = s.Store.FinishJob(jobID, "failed", msg, "", 0, 0, false)
		_ = s.Store.SetDomainStatus(d.ID, "error", msg)
		_ = s.Store.TouchDomainScan(d.ID)
		return 0, nil
	}
	if err := s.Store.InsertResults(jobID, merged); err != nil {
		return 0, fmt.Errorf("保存结果失败: %w", err)
	}

	top := merged[0]
	note := ""
	if failures > 0 {
		note = fmt.Sprintf("（%d / %d 个探针未返回）", failures, len(tasks))
	}
	logx.Infof(d.UserID, d.ID, "%s 聚合完成 · %d 个探针 · 节点 %d · 最优 %s（%.0fms / %.1fMbps / %s）%s",
		name, len(sets), len(merged), top.IP, top.Latency, top.Speed, top.Colo, note)

	pushed := false
	pushCount := 0
	if d.AutoPush {
		n, err := s.PushResults(ctx, d, merged)
		if err != nil {
			logx.Warnf(d.UserID, d.ID, "%s 自动下发失败 · %s", name, err.Error())
			_ = s.Store.SetDomainStatus(d.ID, "error", "下发失败: "+err.Error())
		} else {
			pushed = true
			pushCount = n
			_ = s.Store.SetDomainStatus(d.ID, "ok", "")
		}
	} else {
		_ = s.Store.SetDomainStatus(d.ID, "ok", "")
	}

	_ = s.Store.UpdateJobProgress(jobID, clampCount(testedTotal), len(merged), 100, "完成")
	_ = s.Store.FinishJob(jobID, "done", "", top.IP, top.Latency, top.Speed, pushed)
	_ = s.Store.TouchDomainScan(d.ID)

	s.Hub.Publish(scanner.Event{
		Type: "done", JobID: jobID, DomainID: d.ID, Stage: "完成",
		Tested: testedTotal, Total: testedTotal, OK: len(merged), Progress: 100,
		Msg: "聚合 " + strconv.Itoa(len(sets)) + " 个探针，最优 " + top.IP + note,
	})
	return pushCount, nil
}

// mergeProbeResults takes the worst value of each metric per IP.
//
// Worst-case, not average: an IP that is 30ms for one carrier and 200ms for
// another must not win just because the average looks fine. The user asking
// for multi-probe aggregation wants a floor, not a mean.
func mergeProbeResults(sets [][]model.ScanResult) []model.ScanResult {
	type agg struct {
		row   model.ScanResult
		colos map[string]struct{}
		seen  int
	}
	if len(sets) == 0 {
		return nil
	}
	byIP := make(map[string]*agg, len(sets[0]))

	for _, set := range sets {
		for _, r := range set {
			a, ok := byIP[r.IP]
			if !ok {
				a = &agg{row: r, colos: map[string]struct{}{}}
				byIP[r.IP] = a
			}
			a.seen++
			// 每个指标取最差值；速度取最低的已测值
			if r.Latency > a.row.Latency {
				a.row.Latency = r.Latency
			}
			if r.Jitter > a.row.Jitter {
				a.row.Jitter = r.Jitter
			}
			if r.Loss > a.row.Loss {
				a.row.Loss = r.Loss
			}
			if r.HTTPRTT > a.row.HTTPRTT {
				a.row.HTTPRTT = r.HTTPRTT
			}
			if r.Speed > 0 && (a.row.Speed == 0 || r.Speed < a.row.Speed) {
				a.row.Speed = r.Speed
			}
			if r.Colo != "" {
				a.colos[r.Colo] = struct{}{}
			}
		}
	}

	out := make([]model.ScanResult, 0, len(byIP))
	for ip, a := range byIP {
		row := a.row
		row.IP = ip
		row.OK = true
		row.ID = 0
		row.JobID = 0
		if len(a.colos) > 0 {
			list := make([]string, 0, len(a.colos))
			for c := range a.colos {
				list = append(list, c)
			}
			sort.Strings(list)
			row.Colo = strings.Join(list, "/")
		} else {
			row.Colo = ""
		}
		// 用合并后的最差值重算评分，而不是沿用某个探针的分数
		row.Score = math.Round(scanner.Score(row.Latency, row.Loss/100, row.Speed, row.HTTPRTT)*100) / 100
		out = append(out, row)
	}
	scanner.SortResults(out)
	return out
}

// sanitizeProbeResults treats the agent as untrusted input. Results end up in
// DNS A records, so a malformed or hostile payload must never reach the store.
func sanitizeProbeResults(in []model.ScanResult) []model.ScanResult {
	const maxResults = 500
	out := make([]model.ScanResult, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, r := range in {
		if len(out) >= maxResults {
			break
		}
		ip := net.ParseIP(strings.TrimSpace(r.IP))
		// A records only: an IPv6 answer here would be written as an A record
		if ip == nil || ip.To4() == nil {
			continue
		}
		key := ip.String()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		r.IP = key
		r.Latency = clampF(r.Latency, 0, 60000)
		r.Jitter = clampF(r.Jitter, 0, 60000)
		r.Loss = clampF(r.Loss, 0, 100)
		r.HTTPRTT = clampF(r.HTTPRTT, 0, 60000)
		r.Speed = clampF(r.Speed, 0, 100000)
		r.Score = clampF(r.Score, 0, 100)
		r.Colo = normColo(r.Colo)
		r.OK = r.Latency > 0
		r.ID = 0
		r.JobID = 0
		out = append(out, r)
	}
	return out
}

// clampCount keeps a misbehaving agent from writing absurd counters into the
// job table (the UI shows them directly).
func clampCount(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100000 {
		return 100000
	}
	return n
}

func clampF(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return math.Round(v*100) / 100
}

func normColo(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != 3 {
		return ""
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return ""
		}
	}
	return s
}

// ProbeJanitor releases scan slots held by tasks that never came back. Without
// it a crashed agent blocks its domain forever, because "running" is only
// cleared when a result arrives.
func (s *Service) ProbeJanitor(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.expireProbeTasks(ctx)
		}
	}
}

func (s *Service) expireProbeTasks(ctx context.Context) {
	nowT := time.Now().Unix()
	// 排队 5 分钟没人领 = 探针不在；执行 15 分钟没回传 = 探针跑挂了。
	// 探针自己最多跑 12 分钟，15 分钟留 3 分钟余量，既不会误杀慢扫描，
	// 也不至于让一台探针掉线就把域名卡 20 分钟
	expired, err := s.Store.ExpireProbeTasks(nowT-5*60, nowT-15*60)
	if err != nil || len(expired) == 0 {
		return
	}
	// 同一个 job 可能一次超时多个任务，按 job 去重后统一结算
	seen := map[int64]bool{}
	for _, task := range expired {
		if seen[task.JobID] {
			continue
		}
		seen[task.JobID] = true

		d, derr := s.Store.DomainByIDInternal(task.DomainID)
		if derr != nil {
			continue
		}
		logx.Warnf(d.UserID, d.ID, "%s 探针任务超时（任务 #%d）：%s", d.FullName(), task.ID, task.Error)
		s.Hub.Publish(scanner.Event{Type: "progress", JobID: task.JobID, DomainID: d.ID,
			Stage: "等待探针", Msg: "探针超时：" + task.Error})
		_, _ = s.finishJobIfReady(ctx, task.JobID, d)
	}
}
