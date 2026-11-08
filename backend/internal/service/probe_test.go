package service

import (
	"testing"

	"edgepilot/internal/model"
)

// A probe is user-controlled input and its results become DNS A records, so
// anything malformed has to be dropped before it reaches the store.
func TestSanitizeProbeResults(t *testing.T) {
	in := []model.ScanResult{
		{IP: "1.2.3.4", Latency: 50, Score: 90, Colo: "hkg"},
		{IP: "1.2.3.4", Latency: 60, Score: 80, Colo: "HKG"}, // duplicate
		{IP: "not-an-ip", Latency: 10, Score: 99},
		{IP: "2001:db8::1", Latency: 20, Score: 95}, // IPv6 must not become an A record
		{IP: " 5.6.7.8 ", Latency: 4000, Loss: 150, Score: 120, Colo: "hkgx"},
	}
	out := sanitizeProbeResults(in)

	if len(out) != 2 {
		t.Fatalf("expected 2 valid results, got %d: %+v", len(out), out)
	}
	if out[0].IP != "1.2.3.4" || out[0].Colo != "HKG" {
		t.Fatalf("first result wrong: %+v", out[0])
	}
	second := out[1]
	if second.IP != "5.6.7.8" {
		t.Fatalf("whitespace not trimmed: %q", second.IP)
	}
	if second.Loss != 100 {
		t.Fatalf("loss must clamp to 100, got %v", second.Loss)
	}
	if second.Score != 100 {
		t.Fatalf("score must clamp to 100, got %v", second.Score)
	}
	if second.Colo != "" {
		t.Fatalf("4-letter colo must be rejected, got %q", second.Colo)
	}
}

func TestSanitizeDropsNaN(t *testing.T) {
	nan := 0.0
	nan = nan / nan // NaN without importing math
	out := sanitizeProbeResults([]model.ScanResult{{IP: "1.1.1.1", Latency: 10, Score: nan}})
	if len(out) != 1 {
		t.Fatalf("expected 1 result, got %d", len(out))
	}
	if out[0].Score != 0 {
		t.Fatalf("NaN must become 0, got %v", out[0].Score)
	}
}

func TestProbeOptionsRoundTrip(t *testing.T) {
	o := ProbeOptions{
		Candidates: 120, Concurrency: 32, Samples: 3, TimeoutMs: 1500,
		SpeedTop: 6, SpeedBytes: 1 << 20, Port: 443,
		LatencyMax: 300, SpeedFloor: 20, SpeedOK: 2,
		ColoAllow: []string{"HKG"}, NoSpeedTest: true,
		Seeds: []string{"1.2.3.4"},
	}
	got := o.Options("cdn.example.com")
	if got.Host != "cdn.example.com" || got.Candidates != 120 || got.SpeedOK != 2 {
		t.Fatalf("round trip lost fields: %+v", got)
	}
	if !got.NoSpeedTest || len(got.ColoAllow) != 1 || got.ColoAllow[0] != "HKG" {
		t.Fatalf("policy fields lost: %+v", got)
	}
}

func TestEffectiveConcurrency(t *testing.T) {
	cases := []struct{ global, probe, want int }{
		{48, 0, 48},  // 0 = follow global
		{48, 16, 16}, // home broadband cap wins
		{48, 64, 48}, // a probe may not raise the global limit
		{48, 48, 48}, // equal is a no-op
		{48, -1, 48}, // defensive: negative falls back to global
	}
	for _, c := range cases {
		if got := effectiveConcurrency(c.global, c.probe); got != c.want {
			t.Fatalf("effectiveConcurrency(%d,%d)=%d, want %d", c.global, c.probe, got, c.want)
		}
	}
}

// The whole point of multi-probe aggregation: an IP that is great for one
// vantage point and terrible for another must lose to one that is merely good
// for both. Averaging would pick the former.
func TestMergeProbeResultsTakesWorstCase(t *testing.T) {
	a := []model.ScanResult{
		{IP: "1.1.1.1", Latency: 30, Loss: 0, Speed: 50, HTTPRTT: 100, Colo: "HKG"},
		{IP: "2.2.2.2", Latency: 100, Loss: 0, Speed: 50, HTTPRTT: 350, Colo: "HKG"},
	}
	b := []model.ScanResult{
		{IP: "1.1.1.1", Latency: 200, Loss: 0, Speed: 50, HTTPRTT: 700, Colo: "NRT"},
		{IP: "2.2.2.2", Latency: 100, Loss: 0, Speed: 50, HTTPRTT: 350, Colo: "HKG"},
	}
	got := mergeProbeResults([][]model.ScanResult{a, b})
	if len(got) != 2 {
		t.Fatalf("expected 2 merged rows, got %d", len(got))
	}
	if got[0].IP != "2.2.2.2" {
		t.Fatalf("stable IP should win, got %s first (scores: %v/%v)",
			got[0].IP, got[0].Score, got[1].Score)
	}

	var volatile model.ScanResult
	for _, r := range got {
		if r.IP == "1.1.1.1" {
			volatile = r
		}
	}
	if volatile.Latency != 200 {
		t.Fatalf("latency must be the worst value, got %v", volatile.Latency)
	}
	if volatile.HTTPRTT != 700 {
		t.Fatalf("http rtt must be the worst value, got %v", volatile.HTTPRTT)
	}
	if volatile.Colo != "HKG/NRT" {
		t.Fatalf("colo must union both vantage points, got %q", volatile.Colo)
	}
}

func TestMergeProbeResultsSingleSetPassesThrough(t *testing.T) {
	one := []model.ScanResult{
		{IP: "9.9.9.9", Latency: 50, Loss: 0, Speed: 40, HTTPRTT: 180, Colo: "HKG"},
		{IP: "8.8.8.8", Latency: 300, Loss: 0, Speed: 0, HTTPRTT: 0, Colo: ""},
	}
	got := mergeProbeResults([][]model.ScanResult{one})
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	if got[0].IP != "9.9.9.9" {
		t.Fatalf("verified node should rank first, got %s", got[0].IP)
	}
	if got[1].Score >= got[0].Score {
		t.Fatalf("unverified node must score lower: %v vs %v", got[1].Score, got[0].Score)
	}
}

func TestMergeProbeResultsKeepsSpeedMinimum(t *testing.T) {
	a := []model.ScanResult{{IP: "1.1.1.1", Latency: 50, Speed: 80, HTTPRTT: 200}}
	b := []model.ScanResult{{IP: "1.1.1.1", Latency: 50, Speed: 12, HTTPRTT: 200}}
	got := mergeProbeResults([][]model.ScanResult{a, b})
	if got[0].Speed != 12 {
		t.Fatalf("speed must be the worst measured value, got %v", got[0].Speed)
	}
}
