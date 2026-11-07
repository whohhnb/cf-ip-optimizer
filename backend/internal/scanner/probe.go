package scanner

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

func ipToBigInt(ip net.IP) *big.Int {
	if v4 := ip.To4(); v4 != nil {
		return new(big.Int).SetBytes(v4)
	}
	return new(big.Int).SetBytes(ip.To16())
}

func newBigInt(v uint64) *big.Int { return new(big.Int).SetUint64(v) }

func bigIntToIP(v *big.Int, size int) net.IP {
	b := v.Bytes()
	if size == 4 {
		if len(b) > 4 {
			return nil
		}
		out := make(net.IP, 4)
		copy(out[4-len(b):], b)
		return out
	}
	if len(b) > 16 {
		return nil
	}
	out := make(net.IP, 16)
	copy(out[16-len(b):], b)
	return out
}

// TCPProbe measures handshake latency against a single address.
// It returns median latency, jitter (max-min) and loss ratio in [0,1].
func TCPProbe(ctx context.Context, ip string, port int, timeout time.Duration, samples int) (latency, jitter, loss float64) {
	if samples <= 0 {
		samples = 3
	}
	addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
	lats := make([]float64, 0, samples)
	fails := 0
	for i := 0; i < samples; i++ {
		if ctx.Err() != nil {
			break
		}
		d := net.Dialer{Timeout: timeout}
		start := time.Now()
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			fails++
			continue
		}
		lats = append(lats, float64(time.Since(start).Microseconds())/1000.0)
		conn.Close()
		// small stagger avoids SYN bursts looking like packet loss
		if i < samples-1 {
			select {
			case <-ctx.Done():
			case <-time.After(30 * time.Millisecond):
			}
		}
	}
	loss = float64(fails) / float64(samples)
	if len(lats) == 0 {
		return 0, 0, loss
	}
	// 中位数而不是均值：跨境链路上一次 500ms 的瞬时排队就能把均值拉高
	// 一倍，而这个 IP 实际上完全可用
	sort.Float64s(lats)
	n := len(lats)
	med := lats[n/2]
	if n%2 == 0 {
		med = (lats[n/2-1] + lats[n/2]) / 2
	}
	return med, lats[n-1] - lats[0], loss
}

// pinnedTransport forces every connection to a specific edge IP while keeping
// TLS SNI and the Host header for the real hostname.
func pinnedTransport(ip, sni string, timeout time.Duration) *http.Transport {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
		},
		TLSClientConfig: &tls.Config{
			ServerName: sni,
			MinVersion: tls.VersionTLS12,
		},
		TLSHandshakeTimeout: timeout,
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        1,
	}
}

// Trace hits /cdn-cgi/trace through the given edge IP and returns the colo code
// plus the round trip of the HTTP request.
func Trace(ctx context.Context, ip, host string, timeout time.Duration) (colo string, rtt float64, err error) {
	sni := host
	tr := pinnedTransport(ip, sni, timeout)
	defer tr.CloseIdleConnections()

	client := &http.Client{Transport: tr, Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/cdn-cgi/trace", nil)
	if err != nil {
		return "", 0, err
	}
	req.Host = host
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	rtt = float64(time.Since(start).Microseconds()) / 1000.0
	if resp.StatusCode != 200 {
		return "", rtt, fmt.Errorf("status %d", resp.StatusCode)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "colo=") {
			colo = strings.TrimPrefix(line, "colo=")
		}
	}
	return colo, rtt, nil
}

// coloFromHeader pulls the IATA code out of Cloudflare's cf-ray header
// ("7bd32409eda7b020-SJC"). Reading it here saves the extra round trip a
// dedicated /cdn-cgi/trace request would cost.
func coloFromHeader(h http.Header) string {
	ray := h.Get("Cf-Ray")
	i := strings.LastIndex(ray, "-")
	if i < 0 || len(ray)-i-1 != 3 {
		return ""
	}
	code := ray[i+1:]
	for _, c := range code {
		if c < 'A' || c > 'Z' {
			return ""
		}
	}
	return code
}

const downloadUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// Download measures throughput in Mbps by pulling `bytes` from the edge, and
// returns the colo code read from the response headers.
//
// Two details decide whether the number is meaningful:
//   - the clock starts after the response headers arrive, so the TCP and TLS
//     handshakes never enter the window
//   - the first slice of the body is discarded, because TCP slow start makes
//     the opening tens of milliseconds far slower than the sustained rate
//
// Without both, a 3 MB sample reads 20-30% low on a fast link and the ranking
// skews towards slow nodes.
func Download(ctx context.Context, ip string, bytes int, timeout time.Duration) (mbps float64, colo string, err error) {
	const host = "speed.cloudflare.com"
	tr := pinnedTransport(ip, host, timeout)
	defer tr.CloseIdleConnections()

	client := &http.Client{Transport: tr, Timeout: timeout}
	url := fmt.Sprintf("https://%s/__down?bytes=%d", host, bytes)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	req.Host = host
	req.Header.Set("User-Agent", downloadUA)
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, "", fmt.Errorf("status %d", resp.StatusCode)
	}
	colo = coloFromHeader(resp.Header)

	// headers are in: handshake is done, start the clock here
	start := time.Now()

	// skip roughly an eighth of the payload (capped at 256 KiB) as warm-up
	warm := bytes / 8
	if warm > 256<<10 {
		warm = 256 << 10
	}
	if warm < 32<<10 {
		warm = 32 << 10
	}

	body := io.LimitReader(resp.Body, int64(bytes))
	buf := make([]byte, 64<<10)

	var (
		total     int64
		warmBytes int64
		warmAt    time.Time
		lastAt    time.Time
	)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			total += int64(n)
			lastAt = time.Now()
			if warmAt.IsZero() && total >= int64(warm) {
				warmAt, warmBytes = lastAt, total
			}
		}
		if rerr != nil {
			// io.EOF means we got the whole payload; anything else still
			// leaves us with the bytes read so far, which is usable
			break
		}
	}
	if lastAt.IsZero() {
		return 0, colo, fmt.Errorf("未收到数据")
	}

	measureBytes := total - warmBytes
	measureDur := lastAt.Sub(warmAt)
	if warmAt.IsZero() || measureBytes <= 0 || measureDur <= 0 {
		// payload too small to warm up in: fall back to the whole window
		measureBytes = total
		measureDur = lastAt.Sub(start)
	}
	if measureDur <= 0 {
		return 0, colo, nil
	}
	return (float64(measureBytes) * 8 / 1e6) / measureDur.Seconds(), colo, nil
}
