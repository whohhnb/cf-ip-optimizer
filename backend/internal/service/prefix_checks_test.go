package service

import (
	"path/filepath"
	"testing"

	"edgepilot/internal/model"
	"edgepilot/internal/store"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Service{Store: st}
}

func TestMedianFloat(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{
		{nil, 0},
		{[]float64{50}, 50},
		{[]float64{50, 90, 70}, 70},
		{[]float64{10, 20, 30, 40}, 25},
	}
	for _, c := range cases {
		if got := medianFloat(c.in); got != c.want {
			t.Errorf("medianFloat(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// 观测必须用中位数，不能再用「最快的一次」——刚被 20 次实测证明会误导。
func TestRecordPrefixChecksUsesMedianAndCollectsColos(t *testing.T) {
	svc := newSvc(t)
	d := &model.Domain{RecordLine: "电信"}
	if err := svc.Store.ReplaceCandidatePrefixes("电信", []string{"172.64.53.0/24"}); err != nil {
		t.Fatal(err)
	}

	results := []model.ScanResult{
		{IP: "172.64.53.1", Latency: 50, Colo: "NRT", OK: true},
		{IP: "172.64.53.2", Latency: 90, Colo: "NRT", OK: true},
		{IP: "172.64.53.3", Latency: 70, Colo: "LAX", OK: false},
	}
	svc.recordPrefixChecks(d, results)

	checks, err := svc.Store.ListPrefixChecks()
	if err != nil {
		t.Fatal(err)
	}
	got := checks["电信"][0]
	if got.Latency != 70 {
		t.Errorf("Latency = %v, want 中位数 70（不是最快的 50）", got.Latency)
	}
	if got.Samples != 3 || got.OKCount != 2 {
		t.Errorf("Samples/OKCount = %d/%d, want 3/2", got.Samples, got.OKCount)
	}
	if got.Colo != "LAX/NRT" {
		t.Errorf("Colo = %q, want 落点集合 LAX/NRT", got.Colo)
	}
}

// 本轮某段没有结果时，必须覆盖掉旧观测，否则设置页会一直显示过期落点。
func TestRecordPrefixChecksOverwritesStaleObservation(t *testing.T) {
	svc := newSvc(t)
	d := &model.Domain{RecordLine: "电信"}
	prefixes := []string{"172.64.53.0/24", "108.162.198.0/24"}
	if err := svc.Store.ReplaceCandidatePrefixes("电信", prefixes); err != nil {
		t.Fatal(err)
	}

	svc.recordPrefixChecks(d, []model.ScanResult{
		{IP: "172.64.53.1", Latency: 50, Colo: "NRT", OK: true},
		{IP: "108.162.198.1", Latency: 52, Colo: "NRT", OK: true},
	})
	// 第二轮只有第一个段有结果
	svc.recordPrefixChecks(d, []model.ScanResult{
		{IP: "172.64.53.2", Latency: 60, Colo: "NRT", OK: true},
	})

	checks, _ := svc.Store.ListPrefixChecks()
	if len(checks["电信"]) != 2 {
		t.Fatalf("观测条数 = %d, want 2", len(checks["电信"]))
	}
	byPrefix := map[string]model.PrefixCheck{}
	for _, c := range checks["电信"] {
		byPrefix[c.Prefix] = c
	}
	if stale := byPrefix["108.162.198.0/24"]; stale.Samples != 0 || stale.Colo != "" {
		t.Errorf("未入选的段仍留着旧观测: %+v", stale)
	}
	if fresh := byPrefix["172.64.53.0/24"]; fresh.Latency != 60 {
		t.Errorf("有结果的段没更新: %+v", fresh)
	}
}

// 观测绝不能改动配置表本身。
func TestRecordPrefixChecksNeverTouchesConfig(t *testing.T) {
	svc := newSvc(t)
	d := &model.Domain{RecordLine: "移动"}
	before := []string{"104.21.90.0/24", "172.66.142.0/24"}
	if err := svc.Store.ReplaceCandidatePrefixes("移动", before); err != nil {
		t.Fatal(err)
	}
	svc.recordPrefixChecks(d, []model.ScanResult{
		{IP: "104.21.90.1", Latency: 73, Colo: "HKG", OK: true},
	})
	got := svc.Store.CandidatePrefixes("移动")
	if len(got) != len(before) || got[0] != before[0] || got[1] != before[1] {
		t.Fatalf("配置被改动了: %v -> %v", before, got)
	}
}

func TestRecordPrefixChecksNoConfigIsNoop(t *testing.T) {
	svc := newSvc(t)
	svc.recordPrefixChecks(&model.Domain{RecordLine: "联通"}, []model.ScanResult{
		{IP: "172.64.53.1", Latency: 50, Colo: "NRT", OK: true},
	})
	checks, _ := svc.Store.ListPrefixChecks()
	if len(checks) != 0 {
		t.Fatalf("没有配置时不应产生观测: %v", checks)
	}
}
