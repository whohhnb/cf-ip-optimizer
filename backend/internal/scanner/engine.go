package scanner

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"edgepilot/internal/model"
)

type Options struct {
	Candidates  int
	Concurrency int
	Samples     int
	Timeout     time.Duration
	SpeedTop    int
	SpeedBytes  int
	Port        int
	IPv6        bool
	Host        string // target hostname used for SNI + /cdn-cgi/trace
}

func (o *Options) normalize() {
	if o.Candidates <= 0 {
		o.Candidates = 300
	}
	if o.Candidates > 3000 {
		o.Candidates = 3000
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 48
	}
	if o.Concurrency > 256 {
		o.Concurrency = 256
	}
	if o.Samples <= 0 {
		o.Samples = 3
	}
	if o.Timeout <= 0 {
		o.Timeout = 1200 * time.Millisecond
	}
	if o.SpeedTop <= 0 {
		o.SpeedTop = 12
	}
	if o.SpeedBytes <= 0 {
		o.SpeedBytes = 3 << 20
	}
	if o.Port == 0 {
		o.Port = 443
	}
}

type Progress struct {
	Stage  string
	Tested int
	Total  int
	OK     int
	Msg    string
}

type Outcome struct {
	Results []model.ScanResult
	Tested  int
	OK      int
}

// Run executes the full pipeline: sample -> TCP latency -> edge verification ->
// throughput -> scoring.
func Run(ctx context.Context, opts Options, emit func(Progress)) (Outcome, error) {
	opts.normalize()
	out := Outcome{}

	emit(Progress{Stage: "采样", Msg: "拉取 Cloudflare 地址段"})
	ranges := Ranges(ctx, opts.IPv6)
	if len(ranges) == 0 {
		return out, errNoRanges
	}
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	ips := Sample(ranges, opts.Candidates, rnd)
	if len(ips) == 0 {
		return out, errNoRanges
	}
	total := len(ips)
	emit(Progress{Stage: "延迟测速", Total: total, Msg: "开始 TCP 握手探测"})

	type res struct {
		ip      string
		latency float64
		jitter  float64
		loss    float64
		ok      bool
	}
	results := make([]res, total)
	var tested, okCount int64

	work := make(chan int)
	var wg sync.WaitGroup

	var lastEmit int64
	throttle := func() {
		nowN := time.Now().UnixMilli()
		if nowN-atomic.LoadInt64(&lastEmit) < 250 {
			return
		}
		atomic.StoreInt64(&lastEmit, nowN)
		t := int(atomic.LoadInt64(&tested))
		o := int(atomic.LoadInt64(&okCount))
		pct := 0
		if total > 0 {
			pct = t * 100 / total
		}
		emit(Progress{Stage: "延迟测速", Tested: t, Total: total, OK: o, Msg: "已探测 " + itoa(t) + " / " + itoa(total) + "（" + itoa(pct) + "%）"})
	}

	workers := opts.Concurrency
	if workers > total {
		workers = total
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				if ctx.Err() != nil {
					return
				}
				ip := ips[idx]
				lat, jit, loss := TCPProbe(ctx, ip, opts.Port, opts.Timeout, opts.Samples)
				alive := loss < 1
				results[idx] = res{ip: ip, latency: lat, jitter: jit, loss: loss, ok: alive}
				atomic.AddInt64(&tested, 1)
				if alive {
					atomic.AddInt64(&okCount, 1)
				}
				throttle()
			}
		}()
	}
	for i := range ips {
		if ctx.Err() != nil {
			break
		}
		work <- i
	}
	close(work)
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return out, err
	}

	// keep only responsive candidates, best latency first
	alive := make([]res, 0, len(results))
	for _, r := range results {
		if r.ok && r.latency > 0 {
			alive = append(alive, r)
		}
	}
	sort.Slice(alive, func(i, j int) bool { return alive[i].latency < alive[j].latency })
	out.Tested = int(atomic.LoadInt64(&tested))
	out.OK = len(alive)

	// phase 3: verify the shortlist really serves HTTP through this edge
	shortlist := alive
	if len(shortlist) > opts.SpeedTop*3 {
		shortlist = shortlist[:opts.SpeedTop*3]
	}
	emit(Progress{Stage: "可用性校验", Tested: out.Tested, Total: total, OK: out.OK, Msg: "校验 HTTP 边缘节点"})

	verified := make([]res, 0, len(shortlist))
	colos := make(map[string]string, len(shortlist))
	for _, r := range shortlist {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		colo, _, err := Trace(ctx, r.ip, opts.Host, opts.Timeout*2)
		if err != nil {
			continue
		}
		colos[r.ip] = colo
		verified = append(verified, r)
	}
	if len(verified) == 0 {
		// fall back to raw latency ranking if trace is blocked
		verified = shortlist
	}
	if len(verified) > opts.SpeedTop {
		verified = verified[:opts.SpeedTop]
	}

	// phase 4: throughput on the finalists
	emit(Progress{Stage: "下载测速", Tested: out.Tested, Total: total, OK: out.OK, Msg: "对 Top 节点测速"})
	speeds := make(map[string]float64, len(verified))
	for i, r := range verified {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		mbps, err := Download(ctx, r.ip, opts.SpeedBytes, 6*time.Second)
		if err == nil {
			speeds[r.ip] = mbps
		}
		emit(Progress{Stage: "下载测速", Tested: out.Tested, Total: total, OK: out.OK,
			Msg: "测速 " + itoa(i+1) + " / " + itoa(len(verified))})
	}

	// phase 5: score
	for _, r := range alive {
		sr := model.ScanResult{
			IP:      r.ip,
			Latency: round2(r.latency),
			Jitter:  round2(r.jitter),
			Loss:    round2(r.loss * 100),
			OK:      true,
			Colo:    colos[r.ip],
		}
		if sp, ok := speeds[r.ip]; ok {
			sr.Speed = round2(sp)
		}
		sr.Score = round2(score(r.latency, r.loss, sr.Speed))
		out.Results = append(out.Results, sr)
	}
	sort.Slice(out.Results, func(i, j int) bool {
		if math.Abs(out.Results[i].Score-out.Results[j].Score) < 0.01 {
			return out.Results[i].Latency < out.Results[j].Latency
		}
		return out.Results[i].Score > out.Results[j].Score
	})
	emit(Progress{Stage: "完成", Tested: out.Tested, Total: total, OK: out.OK, Msg: "扫描完成"})
	return out, nil
}

// score blends latency, throughput and packet loss into 0..100.
// Candidates that never reached the throughput phase are ranked on the two
// metrics we do have, scaled so they cannot leapfrog a measured node.
func score(latencyMs, loss float64, speedMbps float64) float64 {
	latScore := clamp01(1 - (latencyMs-10)/490)
	lossScore := clamp01(1 - loss)
	if speedMbps <= 0 {
		return 100 * (0.65*latScore + 0.35*lossScore) * 0.92
	}
	spdScore := clamp01(speedMbps / 50)
	return 100 * (0.45*latScore + 0.30*spdScore + 0.25*lossScore)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

var errNoRanges = &scanErr{"无法获取 Cloudflare 地址段"}

type scanErr struct{ s string }

func (e *scanErr) Error() string { return e.s }

// HealthCheck is a cheap liveness probe used by the scheduler watchdog.
func HealthCheck(ctx context.Context, ip string, port int, timeout time.Duration) bool {
	lat, _, loss := TCPProbe(ctx, ip, port, timeout, 2)
	return loss < 1 && lat > 0
}
