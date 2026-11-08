package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
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

// dispatchToProbe queues a scan for the remote agent bound to this domain.
// It returns an error instead of silently falling back to a local scan: if the
// user asked for a 电信 probe and it is offline, a local result would be the
// wrong vantage point and quietly corrupt the per-line ranking.
func (s *Service) dispatchToProbe(d *model.Domain, job *model.ScanJob, opts scanner.Options) error {
	probe, err := s.Store.ProbeByID(d.ProbeID, d.UserID)
	if err != nil {
		return errors.New("绑定的探针不存在，请重新选择")
	}
	if !probe.Online {
		seen := "从未上线"
		if probe.LastSeenAt != nil {
			seen = "最后心跳 " + probe.LastSeenAt.Format("01-02 15:04")
		}
		return fmt.Errorf("探针「%s」已离线（%s），请检查探针进程", probe.Name, seen)
	}

	body, err := json.Marshal(ProbePayload{Host: d.FullName(), Options: toProbeOptions(opts)})
	if err != nil {
		return err
	}
	if _, err := s.Store.CreateProbeTask(probe.ID, d.ID, job.ID, string(body)); err != nil {
		return fmt.Errorf("创建探针任务失败: %w", err)
	}

	_ = s.Store.UpdateJobProgress(job.ID, 0, 0, 0, "已派发")
	s.Hub.Publish(scanner.Event{
		Type: "progress", JobID: job.ID, DomainID: d.ID, Stage: "已派发",
		Msg: "已派发给探针「" + probe.Name + "」（" + probe.Line + "）",
	})
	logx.Infof(d.UserID, d.ID, "%s 派发探针 · %s（%s）· 候选=%d",
		d.FullName(), probe.Name, probe.Line, opts.Candidates)
	return nil
}

// SubmitProbeResult stores what an agent measured and, if the domain is set to
// auto-push, writes the winning addresses to that domain's record line.
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

	// the agent owns the timing; the controller still releases the scan slot
	defer s.markRunning(d.ID, false)

	if rep.Status == "failed" || strings.TrimSpace(rep.Error) != "" {
		msg := strings.TrimSpace(rep.Error)
		if msg == "" {
			msg = "探针执行失败"
		}
		_ = s.Store.FinishProbeTask(task.ID, "failed", msg)
		_ = s.Store.FinishJob(task.JobID, "failed", msg, "", 0, 0, false)
		_ = s.Store.SetDomainStatus(d.ID, "error", msg)
		_ = s.Store.TouchDomainScan(d.ID)
		logx.Warnf(d.UserID, d.ID, "%s 探针执行失败 · %s", name, msg)
		s.Hub.Publish(scanner.Event{Type: "error", JobID: task.JobID, DomainID: d.ID, Error: msg})
		return 0, nil
	}

	results := sanitizeProbeResults(rep.Results)
	if len(results) == 0 {
		msg := "探针没有返回可用节点"
		_ = s.Store.FinishProbeTask(task.ID, "failed", msg)
		_ = s.Store.FinishJob(task.JobID, "failed", msg, "", 0, 0, false)
		_ = s.Store.SetDomainStatus(d.ID, "error", msg)
		_ = s.Store.TouchDomainScan(d.ID)
		logx.Warnf(d.UserID, d.ID, "%s 探针返回 0 个可用节点", name)
		s.Hub.Publish(scanner.Event{Type: "error", JobID: task.JobID, DomainID: d.ID, Error: msg})
		return 0, nil
	}

	if err := s.Store.InsertResults(task.JobID, results); err != nil {
		return 0, fmt.Errorf("保存结果失败: %w", err)
	}
	// 与本地扫描用同一份排序，否则「已校验优先」会在这一层被抹掉，
	// 未校验的节点会凭更好看的延迟抢走槽位
	scanner.SortResults(results)

	top := results[0]
	logx.Infof(d.UserID, d.ID, "%s 探针回传 · 测试 %d · 可用 %d · 最优 %s（%.0fms / %.1fMbps / %s）· 耗时 %dms",
		name, rep.Tested, len(results), top.IP, top.Latency, top.Speed, top.Colo, rep.ElapsedMs)

	pushed := false
	pushCount := 0
	if d.AutoPush {
		n, err := s.PushResults(ctx, d, results)
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

	_ = s.Store.UpdateJobProgress(task.JobID, clampCount(rep.Tested), len(results), 100, "完成")
	_ = s.Store.FinishJob(task.JobID, "done", "", top.IP, top.Latency, top.Speed, pushed)
	_ = s.Store.FinishProbeTask(task.ID, "done", "")
	_ = s.Store.TouchDomainScan(d.ID)

	s.Hub.Publish(scanner.Event{
		Type: "done", JobID: task.JobID, DomainID: d.ID, Stage: "完成",
		Tested: rep.Tested, Total: rep.Tested, OK: len(results), Progress: 100,
		Msg: "探针回传 " + strconv.Itoa(len(results)) + " 个节点，最优 " + top.IP,
	})
	return pushCount, nil
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
			s.expireProbeTasks()
		}
	}
}

func (s *Service) expireProbeTasks() {
	nowT := time.Now().Unix()
	expired, err := s.Store.ExpireProbeTasks(nowT-5*60, nowT-20*60)
	if err != nil || len(expired) == 0 {
		return
	}
	for _, task := range expired {
		d, derr := s.Store.DomainByIDInternal(task.DomainID)
		if derr != nil {
			continue
		}
		msg := task.Error
		if msg == "" {
			msg = "探针超时未返回结果"
		}
		s.markRunning(d.ID, false)
		_ = s.Store.FinishJob(task.JobID, "failed", msg, "", 0, 0, false)
		_ = s.Store.SetDomainStatus(d.ID, "error", msg)
		_ = s.Store.TouchDomainScan(d.ID)
		logx.Warnf(d.UserID, d.ID, "%s %s（任务 #%d）", d.FullName(), msg, task.ID)
		s.Hub.Publish(scanner.Event{Type: "error", JobID: task.JobID, DomainID: d.ID, Error: msg})
	}
}
