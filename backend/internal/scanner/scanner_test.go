package scanner

import (
	"context"
	"math/rand"
	"os"
	"testing"
	"time"
)

func TestSampleRespectsRanges(t *testing.T) {
	ranges := parseCIDRs(fallbackV4)
	ips := Sample(ranges, 200, rand.New(rand.NewSource(42)))
	if len(ips) != 200 {
		t.Fatalf("expected 200 samples, got %d", len(ips))
	}
	seen := map[string]bool{}
	for _, s := range ips {
		if seen[s] {
			t.Fatalf("duplicate sample %s", s)
		}
		seen[s] = true
	}
}

func TestScoreOrdering(t *testing.T) {
	fast := score(20, 0, 60)
	slow := score(300, 0.5, 1)
	if fast <= slow {
		t.Fatalf("fast node should outrank slow node: %.2f vs %.2f", fast, slow)
	}
	if fast > 100 || slow < 0 {
		t.Fatalf("score out of range: %.2f %.2f", fast, slow)
	}
}

// TestLiveScan hits the real Cloudflare network. Enable with PILOT_LIVE=1.
func TestLiveScan(t *testing.T) {
	if os.Getenv("PILOT_LIVE") != "1" {
		t.Skip("set PILOT_LIVE=1 to run the live network scan")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	opts := Options{
		Candidates:  60,
		Concurrency: 24,
		Samples:     2,
		Timeout:     1500 * time.Millisecond,
		SpeedTop:    3,
		SpeedBytes:  512 << 10,
		Port:        443,
		Host:        "cloudflare.com",
	}
	out, err := Run(ctx, opts, func(p Progress) {})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	t.Logf("tested=%d ok=%d results=%d", out.Tested, out.OK, len(out.Results))
	if len(out.Results) == 0 {
		t.Fatal("no usable node found")
	}
	top := out.Results[0]
	t.Logf("top: ip=%s latency=%.1fms speed=%.1fMbps colo=%s score=%.1f",
		top.IP, top.Latency, top.Speed, top.Colo, top.Score)
}
