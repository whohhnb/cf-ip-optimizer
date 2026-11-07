package scanner

import (
	"context"
	"math/rand"
	"net"
	"net/http"
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

// A pure size-weighted draw lets 104.16.0.0/13 (34% of the space) starve the
// small blocks: a single /22 expects 0.2 hits out of 300. Half the budget is
// split evenly so every block shows up on every scan.
func TestSampleCoversEveryRange(t *testing.T) {
	ranges := parseCIDRs(fallbackV4)
	ips := Sample(ranges, 200, rand.New(rand.NewSource(7)))
	if len(ips) != 200 {
		t.Fatalf("expected 200 samples, got %d", len(ips))
	}

	hit := make(map[string]int, len(ranges))
	for _, s := range ips {
		ip := net.ParseIP(s)
		for _, r := range ranges {
			if ip != nil && r.Contains(ip) {
				hit[r.String()]++
				break
			}
		}
	}
	for _, r := range ranges {
		if hit[r.String()] == 0 {
			t.Fatalf("range %s got no sample out of 200", r)
		}
	}
}

func TestBuildCandidatesReusesSeeds(t *testing.T) {
	ranges := parseCIDRs(fallbackV4)
	seeds := []string{"104.16.1.1", "172.64.2.2", "104.16.1.1"} // duplicate on purpose
	opts := Options{Candidates: 20, Seeds: seeds}
	got := buildCandidates(ranges, opts, rand.New(rand.NewSource(3)))

	if len(got) != 20 {
		t.Fatalf("expected 20 candidates, got %d", len(got))
	}
	seen := map[string]bool{}
	for _, ip := range got {
		if seen[ip] {
			t.Fatalf("duplicate candidate %s", ip)
		}
		seen[ip] = true
	}
	for _, s := range []string{"104.16.1.1", "172.64.2.2"} {
		if !seen[s] {
			t.Fatalf("seed %s missing from candidates", s)
		}
	}
}

func TestScoreOrdering(t *testing.T) {
	fast := score(20, 0, 60, 0)
	slow := score(300, 0.5, 1, 0)
	if fast <= slow {
		t.Fatalf("fast node should outrank slow node: %.2f vs %.2f", fast, slow)
	}
	if fast > 100 || slow < 0 {
		t.Fatalf("score out of range: %.2f %.2f", fast, slow)
	}
}

// A node that answers the SYN in 20ms but needs 600ms for a full HTTP exchange
// is being queued or throttled; it must not outrank a clean one.
func TestScorePenalizesSlowHTTP(t *testing.T) {
	clean := score(40, 0, 40, 160) // ratio 4: handshake + 2 TLS + 1 HTTP
	throttled := score(40, 0, 40, 600)
	if throttled >= clean {
		t.Fatalf("throttled node should score lower: %.2f vs %.2f", throttled, clean)
	}
	if clean-throttled > clean*0.15 {
		t.Fatalf("penalty too aggressive: %.2f vs %.2f", throttled, clean)
	}
}

func TestColoFromHeader(t *testing.T) {
	cases := []struct {
		ray  string
		want string
	}{
		{"7bd32409eda7b020-SJC", "SJC"},
		{"7bd32409eda7b020-HKG", "HKG"},
		{"", ""},
		{"garbage", ""},
		{"abc-DE", ""},   // too short
		{"abc-ABCD", ""}, // too long
		{"abc-sjc", ""},  // lowercase is not an IATA code here
	}
	for _, c := range cases {
		h := http.Header{}
		if c.ray != "" {
			h.Set("Cf-Ray", c.ray)
		}
		if got := coloFromHeader(h); got != c.want {
			t.Fatalf("cf-ray %q -> %q, want %q", c.ray, got, c.want)
		}
	}
}

func TestColoAllowed(t *testing.T) {
	allow := []string{"HKG", "NRT"}
	if !coloAllowed("hkg", allow) {
		t.Fatal("matching must be case insensitive")
	}
	if coloAllowed("LAX", allow) {
		t.Fatal("LAX is not in the allow list")
	}
	if coloAllowed("", allow) {
		t.Fatal("empty colo must never match a whitelist")
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
	t.Logf("top: ip=%s latency=%.1fms http=%.1fms speed=%.1fMbps colo=%s score=%.1f",
		top.IP, top.Latency, top.HTTPRTT, top.Speed, top.Colo, top.Score)
}
