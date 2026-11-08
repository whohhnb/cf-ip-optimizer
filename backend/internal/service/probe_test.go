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
