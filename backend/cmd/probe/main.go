// Command probe is a diagnostics helper: it times each scan phase against a
// single edge IP so hangs can be attributed to a specific step.
//
//	go run ./cmd/probe 162.159.212.131 cloudflare.com
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"edgepilot/internal/scanner"
)

func main() {
	ip := "162.159.212.131"
	host := "cloudflare.com"
	if len(os.Args) > 1 {
		ip = os.Args[1]
	}
	if len(os.Args) > 2 {
		host = os.Args[2]
	}

	ctx := context.Background()

	t0 := time.Now()
	lat, jit, loss := scanner.TCPProbe(ctx, ip, 443, 1500*time.Millisecond, 3)
	fmt.Printf("TCPProbe   latency=%.1fms jitter=%.1fms loss=%.2f  (%s)\n", lat, jit, loss, time.Since(t0).Round(time.Millisecond))

	t0 = time.Now()
	colo, rtt, err := scanner.Trace(ctx, ip, host, 3*time.Second)
	fmt.Printf("Trace      colo=%q rtt=%.1fms err=%v  (%s)\n", colo, rtt, err, time.Since(t0).Round(time.Millisecond))

	t0 = time.Now()
	mbps, err := scanner.Download(ctx, ip, 1<<20, 6*time.Second)
	fmt.Printf("Download   speed=%.2fMbps err=%v  (%s)\n", mbps, err, time.Since(t0).Round(time.Millisecond))
}
