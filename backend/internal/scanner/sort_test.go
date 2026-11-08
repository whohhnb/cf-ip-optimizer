package scanner

import (
	"testing"

	"edgepilot/internal/model"
)

// A node that answered the SYN quickly but never passed the HTTP check must not
// outrank one that is known to serve the hostname, even with a worse score.
func TestSortResultsPutsVerifiedFirst(t *testing.T) {
	results := []model.ScanResult{
		{IP: "9.9.9.9", Latency: 80, Score: 95, HTTPRTT: 0},
		{IP: "1.1.1.1", Latency: 200, Score: 60, HTTPRTT: 700},
		{IP: "5.5.5.5", Latency: 150, Score: 70, HTTPRTT: 0},
	}
	SortResults(results)

	if results[0].IP != "1.1.1.1" {
		t.Fatalf("verified node must rank first, got %s", results[0].IP)
	}
	// 未校验的一组内部仍按评分降序
	if results[1].IP != "9.9.9.9" || results[2].IP != "5.5.5.5" {
		t.Fatalf("unverified tail must be ordered by score: %+v", results)
	}
}
