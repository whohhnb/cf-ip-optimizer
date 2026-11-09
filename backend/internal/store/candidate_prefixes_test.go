package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"edgepilot/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestCandidatePrefixesArePerLine(t *testing.T) {
	st := newTestStore(t)

	if err := st.ReplaceCandidatePrefixes("电信", []string{"172.64.53.0/24", "108.162.198.0/24"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceCandidatePrefixes("移动", []string{"104.19.51.0/24"}); err != nil {
		t.Fatal(err)
	}

	if got := st.CandidatePrefixes("电信"); len(got) != 2 {
		t.Fatalf("电信 = %v, want 2 个", got)
	}
	if got := st.CandidatePrefixes("移动"); len(got) != 1 || got[0] != "104.19.51.0/24" {
		t.Fatalf("移动 = %v", got)
	}
	// 同一个前缀可以同时存在于两条线路，互不影响
	if err := st.ReplaceCandidatePrefixes("联通", []string{"172.64.53.0/24"}); err != nil {
		t.Fatal(err)
	}
	if got := st.CandidatePrefixes("电信"); len(got) != 2 {
		t.Fatalf("给联通配前缀后电信被改动了: %v", got)
	}
}

func TestReplaceCandidatePrefixesOverwrites(t *testing.T) {
	st := newTestStore(t)
	if err := st.ReplaceCandidatePrefixes("电信", []string{"172.64.53.0/24", "172.64.52.0/24"}); err != nil {
		t.Fatal(err)
	}
	// 整体替换：旧清单不能残留
	if err := st.ReplaceCandidatePrefixes("电信", []string{"108.162.198.0/24"}); err != nil {
		t.Fatal(err)
	}
	got := st.CandidatePrefixes("电信")
	if len(got) != 1 || got[0] != "108.162.198.0/24" {
		t.Fatalf("替换后 = %v, want 只剩 108.162.198.0/24", got)
	}
}

func TestDeleteCandidatePrefix(t *testing.T) {
	st := newTestStore(t)
	if err := st.ReplaceCandidatePrefixes("电信", []string{"172.64.53.0/24", "172.64.52.0/24"}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteCandidatePrefix("电信", "172.64.53.0/24"); err != nil {
		t.Fatal(err)
	}
	if got := st.CandidatePrefixes("电信"); len(got) != 1 || got[0] != "172.64.52.0/24" {
		t.Fatalf("删除后 = %v", got)
	}
	if err := st.DeleteCandidatePrefix("电信", "172.64.53.0/24"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("重复删除应返回 ErrNoRows, got %v", err)
	}
}

func TestListCandidatePrefixesGroupsByLine(t *testing.T) {
	st := newTestStore(t)
	_ = st.ReplaceCandidatePrefixes("电信", []string{"172.64.53.0/24"})
	_ = st.ReplaceCandidatePrefixes("移动", []string{"104.19.51.0/24", "104.16.149.0/24"})

	lines, err := st.ListCandidatePrefixes()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines["电信"]) != 1 || len(lines["移动"]) != 2 {
		t.Fatalf("分组结果不对: %v", lines)
	}
}

// 守卫测试：扫描链路里不允许出现任何写候选前缀的调用。
//
// 这是这次设计的核心约束 —— 前缀表是配置，不是记忆。一旦扫描回写，
// 就会变成「用几轮前的结论决定下一轮候选」，把实时测量变成自我验证。
func TestScanPipelineNeverWritesPrefixes(t *testing.T) {
	files := []string{
		"../service/service.go",
		"../service/probe.go",
		"../scanner/engine.go",
		"../scanner/ranges.go",
	}
	forbidden := []string{"ReplaceCandidatePrefixes", "DeleteCandidatePrefix", "RecordPrefix"}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(b)
		for _, bad := range forbidden {
			if strings.Contains(src, bad) {
				t.Errorf("%s 里出现了 %s：扫描流程不允许写候选前缀", f, bad)
			}
		}
	}
}

// 守卫：观测数据不得回流到候选生成。
// 观测表只服务于设置页展示；一旦被 buildCandidates / sampleFromPrefixes 读到，
// 就变成「用历史结论决定下一轮候选」，违背「配置不是记忆」这条红线。
func TestObservationNeverFeedsCandidateGeneration(t *testing.T) {
	files := []string{
		"../scanner/engine.go",
		"../scanner/ranges.go",
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(b)
		for _, bad := range []string{"PrefixCheck", "ListPrefixChecks", "candidate_prefix_checks"} {
			if strings.Contains(src, bad) {
				t.Errorf("%s 里出现了 %s：扫描器不允许读观测数据", f, bad)
			}
		}
	}
}

func TestSaveAndPrunePrefixChecks(t *testing.T) {
	st := newTestStore(t)
	_ = st.ReplaceCandidatePrefixes("电信", []string{"172.64.53.0/24", "108.162.198.0/24"})
	now := time.Now()
	_ = st.SavePrefixChecks("电信", []model.PrefixCheck{
		{Prefix: "172.64.53.0/24", Colo: "NRT", Latency: 52, Samples: 3, OKCount: 3, CheckedAt: now},
		{Prefix: "108.162.198.0/24", Colo: "NRT", Latency: 55, Samples: 2, OKCount: 1, CheckedAt: now},
	})
	lines, err := st.ListPrefixChecks()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines["电信"]) != 2 {
		t.Fatalf("观测条数不对: %+v", lines)
	}
	byPrefix := map[string]model.PrefixCheck{}
	for _, c := range lines["电信"] {
		byPrefix[c.Prefix] = c
	}
	if byPrefix["172.64.53.0/24"].OKCount != 3 || byPrefix["108.162.198.0/24"].OKCount != 1 {
		t.Fatalf("OKCount 读取不对: %+v", lines)
	}
	// 覆盖写，不累积
	_ = st.SavePrefixChecks("电信", []model.PrefixCheck{
		{Prefix: "172.64.53.0/24", Colo: "LAX", Latency: 200, Samples: 1, OKCount: 0, CheckedAt: now},
	})
	lines, _ = st.ListPrefixChecks()
	for _, c := range lines["电信"] {
		if c.Prefix == "172.64.53.0/24" && (c.Colo != "LAX" || c.Latency != 200 || c.OKCount != 0) {
			t.Fatalf("覆盖写失败: %+v", c)
		}
	}
	// 配置删了，观测一起清掉
	_ = st.DeleteCandidatePrefix("电信", "108.162.198.0/24")
	lines, _ = st.ListPrefixChecks()
	for _, c := range lines["电信"] {
		if c.Prefix == "108.162.198.0/24" {
			t.Fatalf("删除配置后仍残留观测: %+v", c)
		}
	}
}
