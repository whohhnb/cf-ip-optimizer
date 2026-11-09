package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"

	"edgepilot/internal/model"
	"edgepilot/internal/scanner"
)

// 候选前缀是运维手工维护的静态配置，每条线路一份。
//
// 这里只有「整体替换」和「删单条」两个写入口，扫描流程从不调用它们：
// 扫描只读这张表，把前缀展开成几个地址去实测，测出来什么就是什么。
// 一旦让扫描回写，就会变成「用几轮前的结论决定下一轮候选」的反馈回路。

type candidatePrefixInput struct {
	Line     string   `json:"line"`
	Prefixes []string `json:"prefixes"`
}

func (s *Server) handleListCandidatePrefixes(w http.ResponseWriter, r *http.Request) {
	lines, err := s.st.ListCandidatePrefixes()
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取候选前缀失败")
		return
	}
	// checks 是最近一次扫描的观测（落点/延迟），只用于展示，不参与候选生成
	checks, err := s.st.ListPrefixChecks()
	if err != nil {
		checks = map[string][]model.PrefixCheck{}
	}
	ok(w, map[string]any{"lines": lines, "checks": checks})
}

func (s *Server) handlePutCandidatePrefixes(w http.ResponseWriter, r *http.Request) {
	var in candidatePrefixInput
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	line := model.LineKey(in.Line)
	if line == "" {
		fail(w, http.StatusBadRequest, "必须指定线路")
		return
	}
	if len(in.Prefixes) > 200 {
		fail(w, http.StatusBadRequest, "单条线路最多 200 个前缀")
		return
	}

	ranges := scanner.CachedRanges()
	seen := make(map[string]struct{}, len(in.Prefixes))
	out := make([]string, 0, len(in.Prefixes))
	for _, raw := range in.Prefixes {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		p := scanner.ParsePrefix(raw)
		if p == nil {
			fail(w, http.StatusBadRequest, "无法识别的地址或前缀："+strings.TrimSpace(raw))
			return
		}
		if !scanner.InRanges(p, ranges) {
			fail(w, http.StatusBadRequest, p.String()+" 不在 Cloudflare 官方地址段内")
			return
		}
		if _, dup := seen[p.String()]; dup {
			continue
		}
		seen[p.String()] = struct{}{}
		out = append(out, p.String())
	}
	sort.Strings(out)

	if err := s.st.ReplaceCandidatePrefixes(line, out); err != nil {
		fail(w, http.StatusInternalServerError, "保存候选前缀失败")
		return
	}
	ok(w, map[string]any{"line": line, "prefixes": out})
}

func (s *Server) handleDeleteCandidatePrefix(w http.ResponseWriter, r *http.Request) {
	line := model.LineKey(r.URL.Query().Get("line"))
	p := scanner.ParsePrefix(r.URL.Query().Get("prefix"))
	if line == "" || p == nil {
		fail(w, http.StatusBadRequest, "line 和 prefix 都是必填")
		return
	}
	if err := s.st.DeleteCandidatePrefix(line, p.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, http.StatusNotFound, "该线路下没有这个前缀")
			return
		}
		fail(w, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(w, map[string]any{"line": line, "prefix": p.String()})
}
