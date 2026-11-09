package scanner

import (
	"math/rand"
	"testing"
)

func TestParsePrefixNormalises(t *testing.T) {
	cases := map[string]string{
		"172.64.53.1":      "172.64.53.0/24",
		"172.64.53.0/24":   "172.64.53.0/24",
		"172.64.53.200/32": "172.64.53.0/24",
		"172.64.0.0/13":    "172.64.0.0/24", // 大段收敛到包含该地址的 /24
		" 108.162.198.33 ": "108.162.198.0/24",
	}
	for in, want := range cases {
		got := ParsePrefix(in)
		if got == nil {
			t.Fatalf("ParsePrefix(%q) = nil", in)
		}
		if got.String() != want {
			t.Errorf("ParsePrefix(%q) = %s, want %s", in, got, want)
		}
	}
	for _, bad := range []string{"", "hello", "::1", "999.1.1.1", "172.64.53.0/33"} {
		if got := ParsePrefix(bad); got != nil {
			t.Errorf("ParsePrefix(%q) = %s, want nil", bad, got)
		}
	}
}

func TestInRanges(t *testing.T) {
	ranges := parseCIDRs(fallbackV4)
	inside := []string{"172.64.53.0/24", "108.162.198.0/24", "104.19.26.0/24", "131.0.72.0/24"}
	for _, p := range inside {
		if !InRanges(ParsePrefix(p), ranges) {
			t.Errorf("%s 应该被判定在官方段内", p)
		}
	}
	for _, p := range []string{"8.8.8.0/24", "1.1.1.0/24", "10.0.0.0/24"} {
		if InRanges(ParsePrefix(p), ranges) {
			t.Errorf("%s 不在官方段内，不该通过", p)
		}
	}
}

// 清单里的每个前缀都必须真的进入候选：这是「静态配置」的全部意义，
// 否则配了等于没配。
func TestBuildCandidatesIncludesEveryPrefix(t *testing.T) {
	ranges := parseCIDRs(fallbackV4)
	prefixes := []string{"172.64.53.0/24", "108.162.198.0/24", "172.64.52.0/24"}
	opts := Options{Candidates: 120, Prefixes: prefixes}
	got := buildCandidates(ranges, opts, rand.New(rand.NewSource(11)))

	if len(got) != 120 {
		t.Fatalf("候选数 = %d, want 120", len(got))
	}
	for _, p := range prefixes {
		found := 0
		for _, ip := range got {
			if PrefixOf(ip) == p {
				found++
			}
		}
		if found == 0 {
			t.Errorf("前缀 %s 没有任何地址进入候选", p)
		}
		if found > 3 {
			t.Errorf("前缀 %s 占了 %d 个候选，超过每段 3 个的上限", p, found)
		}
	}
	// 去重
	seen := map[string]struct{}{}
	for _, ip := range got {
		if _, dup := seen[ip]; dup {
			t.Fatalf("候选里出现重复地址 %s", ip)
		}
		seen[ip] = struct{}{}
	}
}

// 没配清单时必须和旧版行为一致：纯随机、数量正确、无重复。
func TestBuildCandidatesWithoutPrefixes(t *testing.T) {
	ranges := parseCIDRs(fallbackV4)
	got := buildCandidates(ranges, Options{Candidates: 80}, rand.New(rand.NewSource(5)))
	if len(got) != 80 {
		t.Fatalf("候选数 = %d, want 80", len(got))
	}
	seen := map[string]struct{}{}
	for _, ip := range got {
		if _, dup := seen[ip]; dup {
			t.Fatalf("候选里出现重复地址 %s", ip)
		}
		seen[ip] = struct{}{}
	}
}

func TestPrefixBudgetIsBounded(t *testing.T) {
	if got := prefixBudget(300, 300); got != 75 {
		t.Errorf("300 候选的预算 = %d, want 75", got)
	}
	if got := prefixBudget(40, 40); got != 16 {
		t.Errorf("40 候选的预算 = %d, want 16（下限）", got)
	}
	if got := prefixBudget(1000, 1000); got != 80 {
		t.Errorf("1000 候选的预算 = %d, want 80（上限）", got)
	}
	if got := prefixBudget(300, 7); got != 7 {
		t.Errorf("剩余只有 7 时预算 = %d, want 7", got)
	}
}
