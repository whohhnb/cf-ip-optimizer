package scanner

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
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
	minL, maxL, sum := lats[0], lats[0], 0.0
	for _, l := range lats {
		if l < minL {
			minL = l
		}
		if l > maxL {
			maxL = l
		}
		sum += l
	}
	return sum / float64(len(lats)), maxL - minL, loss
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

// Download measures throughput in Mbps by pulling `bytes` from the edge.
// speed.cloudflare.com is used because it serves a predictable payload.
func Download(ctx context.Context, ip string, bytes int, timeout time.Duration) (mbps float64, err error) {
	const host = "speed.cloudflare.com"
	tr := pinnedTransport(ip, host, timeout)
	defer tr.CloseIdleConnections()

	client := &http.Client{Transport: tr, Timeout: timeout}
	url := fmt.Sprintf("https://%s/__down?bytes=%d", host, bytes)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Host = host
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, int64(bytes)))
	elapsed := time.Since(start).Seconds()
	if err != nil && n == 0 {
		return 0, err
	}
	if elapsed <= 0 {
		return 0, nil
	}
	return (float64(n) * 8 / 1e6) / elapsed, nil
}
