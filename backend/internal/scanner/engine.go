package scanner

import (
	"context"
	"math"
	"math/rand"
	"net"
	"sort"
	"strings"
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

	// LatencyMax drops candidates slower than this before the HTTP check
	// runs, so a slow region never eats the download-test budget. 0 = off.
	LatencyMax float64
	// SpeedFloor is the minimum acceptable throughput in Mbps. 0 = off.
	SpeedFloor float64
	// SpeedOK stops the download phase once this many nodes clear SpeedFloor.
	// 0 = measure every finalist.
	SpeedOK int
	// ColoAllow keeps only these IATA codes. Empty = no restriction.
	ColoAllow []string
	// NoSpeedTest skips the throughput phase entirely (latency-only users).
	NoSpeedTest bool
	// Seeds are addresses that scored well last time. Re-testing them beats
	// drawing a fresh random set, because anycast means most new samples land
	// on the same PoP anyway.
	Seeds []string
	// Fixed makes the run measure exactly these addresses and nothing else.
	// The controller sets it when several probes scan for one job: merging
	// results by IP is only meaningful if every probe saw the same list.
	Fixed []string
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
	if o.SpeedOK < 0 {
		o.SpeedOK = 0
	}
	if o.SpeedOK > o.SpeedTop {
		o.SpeedOK = o.SpeedTop
	}
	if o.LatencyMax < 0 {
		o.LatencyMax = 0
	}
	if o.SpeedFloor < 0 {
		o.SpeedFloor = 0
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

type candidate struct {
	ip      string
	latency float64
	jitter  float64
	loss    float64
	colo    string
	httpRTT float64
}

// Run executes the full pipeline: sample -> TCP latency -> edge verification ->
// throughput -> scoring.
func Run(ctx context.Context, opts Options, emit func(Progress)) (Outcome, error) {
	opts.normalize()
	out := Outcome{}

	var ips []string
	if len(opts.Fixed) > 0 {
		emit(Progress{Stage: "采样", Msg: "使用主控下发的固定候选列表"})
		ips = sanitizeFixed(opts.Fixed, opts.Candidates)
	} else {
		emit(Progress{Stage: "采样", Msg: "拉取 Cloudflare 地址段"})
		ranges := Ranges(ctx, opts.IPv6)
		if len(ranges) == 0 {
			return out, errNoRanges
		}
		rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
		ips = buildCandidates(ranges, opts, rnd)
	}
	if len(ips) == 0 {
		return out, errNoRanges
	}
	total := len(ips)
	if len(opts.Seeds) > 0 {
		emit(Progress{Stage: "延迟测速", Total: total,
			Msg: "复用上轮高分 IP " + itoa(len(opts.Seeds)) + " 个，共 " + itoa(total) + " 个候选"})
	} else {
		emit(Progress{Stage: "延迟测速", Total: total, Msg: "开始 TCP 握手探测"})
	}

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
	alive := make([]candidate, 0, len(results))
	droppedSlow := 0
	for _, r := range results {
		if !r.ok || r.latency <= 0 {
			continue
		}
		if opts.LatencyMax > 0 && r.latency > opts.LatencyMax {
			droppedSlow++
			continue
		}
		alive = append(alive, candidate{ip: r.ip, latency: r.latency, jitter: r.jitter, loss: r.loss})
	}
	sort.Slice(alive, func(i, j int) bool { return alive[i].latency < alive[j].latency })
	out.Tested = int(atomic.LoadInt64(&tested))
	out.OK = len(alive)

	// phase 3: verify the shortlist really serves HTTP through this edge
	shortlist := alive
	if len(shortlist) > opts.SpeedTop*3 {
		shortlist = shortlist[:opts.SpeedTop*3]
	}
	msg := "校验 HTTP 边缘节点"
	if droppedSlow > 0 {
		msg = "校验 HTTP 边缘节点（延迟超过 " + itoa(int(opts.LatencyMax)) + "ms 的 " + itoa(droppedSlow) + " 个已剔除）"
	}
	emit(Progress{Stage: "可用性校验", Tested: out.Tested, Total: total, OK: out.OK, Msg: msg})

	verified := make([]candidate, 0, len(shortlist))
	for _, r := range shortlist {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		colo, rtt, err := Trace(ctx, r.ip, opts.Host, opts.Timeout*3)
		if err != nil {
			continue
		}
		if len(opts.ColoAllow) > 0 && !coloAllowed(colo, opts.ColoAllow) {
			continue
		}
		r.colo = colo
		r.httpRTT = rtt
		verified = append(verified, r)
	}
	if len(verified) == 0 {
		if len(opts.ColoAllow) > 0 {
			return out, errColoEmpty
		}
		// trace 被墙时退化为纯延迟排序，至少让用户拿到可用结果
		verified = append(verified, shortlist...)
	}
	emit(Progress{Stage: "可用性校验", Tested: out.Tested, Total: total, OK: out.OK,
		Msg: "HTTP 校验 " + itoa(len(verified)) + " / " + itoa(len(shortlist)) + " 通过"})

	// Trace 已经付过代价了，先把所有校验过的节点的 colo / RTT 记下来，
	// 再裁剪下载队列——否则多测的那些节点信息会白扔
	colos := make(map[string]string, len(verified))
	httpRTTs := make(map[string]float64, len(verified))
	for _, r := range verified {
		colos[r.ip] = r.colo
		httpRTTs[r.ip] = r.httpRTT
	}

	finalists := verified
	if len(finalists) > opts.SpeedTop {
		finalists = finalists[:opts.SpeedTop]
	}

	// phase 4: throughput on the finalists
	speeds := make(map[string]float64, len(finalists))
	if !opts.NoSpeedTest {
		emit(Progress{Stage: "下载测速", Tested: out.Tested, Total: total, OK: out.OK, Msg: "对 Top 节点测速"})
		passed := 0
		for i, r := range finalists {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			mbps, colo, err := Download(ctx, r.ip, opts.SpeedBytes, 8*time.Second)
			if err == nil {
				speeds[r.ip] = mbps
				// 下载响应头里的 cf-ray 是免费的机房信息，trace 失败时补位
				if colo != "" && colos[r.ip] == "" {
					colos[r.ip] = colo
				}
				if opts.SpeedFloor > 0 && mbps >= opts.SpeedFloor {
					passed++
				}
			}
			emit(Progress{Stage: "下载测速", Tested: out.Tested, Total: total, OK: out.OK,
				Msg: "测速 " + itoa(i+1) + " / " + itoa(len(finalists))})
			if opts.SpeedOK > 0 && passed >= opts.SpeedOK {
				emit(Progress{Stage: "下载测速", Tested: out.Tested, Total: total, OK: out.OK,
					Msg: "已有 " + itoa(passed) + " 个达到 " + itoa(int(opts.SpeedFloor)) + " Mbps，提前结束"})
				break
			}
		}
	}

	// phase 5: score
	for _, r := range alive {
		sr := model.ScanResult{
			IP:      r.ip,
			Latency: round2(r.latency),
			Jitter:  round2(r.jitter),
			Loss:    round2(r.loss * 100),
			HTTPRTT: round2(httpRTTs[r.ip]),
			OK:      true,
			Colo:    colos[r.ip],
		}
		if sp, ok := speeds[r.ip]; ok {
			sr.Speed = round2(sp)
		}
		sr.Score = round2(score(r.latency, r.loss, sr.Speed, sr.HTTPRTT))
		out.Results = append(out.Results, sr)
	}
	SortResults(out.Results)
	emit(Progress{Stage: "完成", Tested: out.Tested, Total: total, OK: out.OK, Msg: "扫描完成"})
	return out, nil
}

// sanitizeFixed validates a controller-provided candidate list. It is untrusted
// input in the sense that it crossed a network boundary, so bad entries are
// dropped rather than handed to the dialer.
func sanitizeFixed(in []string, limit int) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		ip := net.ParseIP(strings.TrimSpace(raw))
		if ip == nil {
			continue
		}
		s := ip.String()
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// PickCandidates builds the address list once, so every probe in a job scans
// the same IPs. Two independent random samples out of 1.5M addresses barely
// overlap, which would make per-IP aggregation meaningless.
func PickCandidates(ctx context.Context, n int, ipv6 bool) ([]string, error) {
	ranges := Ranges(ctx, ipv6)
	if len(ranges) == 0 {
		return nil, errNoRanges
	}
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	ips := Sample(ranges, n, rnd)
	if len(ips) == 0 {
		return nil, errNoRanges
	}
	return ips, nil
}

// buildCandidates merges last round's winners with a fresh sample. Re-testing
// known-good addresses is worth more than extra random draws: under anycast
// most new samples resolve to the same PoP, so the marginal value of another
// random IP is near zero while a previously fast one is a proven route.
func buildCandidates(ranges []*net.IPNet, opts Options, rnd *rand.Rand) []string {
	seen := make(map[string]struct{}, opts.Candidates)
	out := make([]string, 0, opts.Candidates)
	add := func(ip string) bool {
		if len(out) >= opts.Candidates {
			return false
		}
		if _, dup := seen[ip]; dup {
			return false
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
		return true
	}
	for _, ip := range opts.Seeds {
		add(ip)
	}
	if rest := opts.Candidates - len(out); rest > 0 {
		for _, ip := range Sample(ranges, rest, rnd) {
			add(ip)
		}
	}
	sort.Strings(out)
	return out
}

func coloAllowed(colo string, allow []string) bool {
	if colo == "" {
		return false
	}
	for _, a := range allow {
		if strings.EqualFold(strings.TrimSpace(a), colo) {
			return true
		}
	}
	return false
}

// score blends latency, throughput, packet loss and the HTTP-layer round trip
// into 0..100. Candidates that never reached the throughput phase are ranked on
// the metrics we do have, scaled so they cannot leapfrog a measured node.
// Score is the canonical 0..100 ranking used by both the local pipeline and
// multi-probe aggregation. Exported so the controller can re-score merged
// worst-case metrics with exactly the same formula.
func Score(latencyMs, loss float64, speedMbps, httpRTT float64) float64 {
	return score(latencyMs, loss, speedMbps, httpRTT)
}

func score(latencyMs, loss float64, speedMbps, httpRTT float64) float64 {
	latScore := clamp01(1 - (latencyMs-10)/490)
	lossScore := clamp01(1 - loss)

	var total float64
	if speedMbps <= 0 {
		total = 100 * (0.65*latScore + 0.35*lossScore) * 0.92
	} else {
		spdScore := clamp01(speedMbps / 50)
		total = 100 * (0.45*latScore + 0.30*spdScore + 0.25*lossScore)
	}

	// A healthy edge answers in about 4 TCP round trips (handshake, two TLS
	// flights, one HTTP). Much more than that means the path is throttled or
	// queueing even though the SYN was fast — a node that looks great on
	// latency but feels slow in a browser.
	if httpRTT > 0 && latencyMs > 0 {
		// TLS 1.3 下正常值在 3.0~3.6（TCP 1 + TLS 1 + HTTP 1），
		// 超过 3.5 开始扣分，到 8 倍扣满 15%
		if ratio := httpRTT / latencyMs; ratio > 3.5 {
			total *= 1 - clamp01((ratio-3.5)/4.5)*0.15
		}
	}
	return total
}

// SortResults applies the canonical ranking: nodes that passed the HTTP check
// first, then by score, then by latency. Both the local pipeline and probe
// ingestion use it, so a remote scan ranks exactly like a local one.
//
// Unverified nodes lose ties on purpose: they may 403, present the wrong
// certificate, or not serve the hostname at all, so a prettier latency number
// must not let them take a DNS slot.
func SortResults(results []model.ScanResult) {
	sort.SliceStable(results, func(i, j int) bool {
		vi := results[i].HTTPRTT > 0
		vj := results[j].HTTPRTT > 0
		if vi != vj {
			return vi
		}
		if math.Abs(results[i].Score-results[j].Score) < 0.01 {
			return results[i].Latency < results[j].Latency
		}
		return results[i].Score > results[j].Score
	})
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

var (
	errNoRanges  = &scanErr{"无法获取 Cloudflare 地址段"}
	errColoEmpty = &scanErr{"按机房白名单过滤后没有可用节点，请放宽限制或关闭白名单"}
)

type scanErr struct{ s string }

func (e *scanErr) Error() string { return e.s }

// HealthCheck is a cheap liveness probe used by the scheduler watchdog.
func HealthCheck(ctx context.Context, ip string, port int, timeout time.Duration) bool {
	lat, _, loss := TCPProbe(ctx, ip, port, timeout, 2)
	return loss < 1 && lat > 0
}
