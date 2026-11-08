package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"edgepilot/internal/logx"
	"edgepilot/internal/model"
	"edgepilot/internal/service"
)

// Probe token format: "ep_" + 48 hex chars. Only the SHA-256 digest is stored,
// so a database leak does not hand over working agent credentials.
func newProbeToken() (token, hash, hint string) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing means the host is broken; refuse rather than
		// issuing a predictable credential
		panic("crypto/rand unavailable: " + err.Error())
	}
	token = "ep_" + hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), token[:9]
}

func hashProbeToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// probeFrom authenticates an agent request. Agents are not users: they carry a
// per-probe token and can only touch their own tasks.
func (s *Server) probeFrom(r *http.Request) (*model.Probe, bool) {
	token := strings.TrimSpace(r.Header.Get("X-Probe-Token"))
	if token == "" {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if len(token) < 16 {
		return nil, false
	}
	p, err := s.st.ProbeByTokenHash(hashProbeToken(token))
	if err != nil {
		return nil, false
	}
	return p, true
}

// ---------- management ----------

type probeInput struct {
	Name string `json:"name"`
	Line string `json:"line"`
}

func (s *Server) handleListProbes(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	list, err := s.st.ListProbes(u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取探针失败")
		return
	}
	ok(w, map[string]any{"probes": list})
}

func (s *Server) handleCreateProbe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in probeInput
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Line = strings.TrimSpace(in.Line)
	if in.Name == "" {
		fail(w, http.StatusBadRequest, "请填写探针名称")
		return
	}
	if len([]rune(in.Name)) > 40 {
		fail(w, http.StatusBadRequest, "名称最长 40 个字符")
		return
	}
	if in.Line == "" {
		in.Line = "默认"
	}
	if len([]rune(in.Line)) > 20 {
		fail(w, http.StatusBadRequest, "线路名过长")
		return
	}

	token, hash, hint := newProbeToken()
	p, err := s.st.CreateProbe(u.ID, in.Name, in.Line, hash, hint)
	if err != nil {
		fail(w, http.StatusInternalServerError, "创建探针失败")
		return
	}
	logx.Logf(u.ID, 0, logx.Info, "新增探针: %s（%s）", p.Name, p.Line)
	// 明文 token 只在这里出现一次，服务端只留哈希
	ok(w, map[string]any{"probe": p, "token": token})
}

func (s *Server) handleDeleteProbe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	n, err := s.st.CountDomainsByProbe(id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if n > 0 {
		fail(w, http.StatusConflict, "还有 "+strconv.Itoa(n)+" 个域名绑定该探针，请先改绑到其他探针或本机")
		return
	}
	if err := s.st.DeleteProbe(id, u.ID); err != nil {
		fail(w, http.StatusNotFound, "探针不存在")
		return
	}
	logx.Logf(u.ID, 0, logx.Info, "删除探针 #%d", id)
	ok(w, map[string]bool{"ok": true})
}

// ---------- agent endpoints ----------

type heartbeatIn struct {
	Version string `json:"version"`
}

func (s *Server) handleProbeHeartbeat(w http.ResponseWriter, r *http.Request) {
	p, authed := s.probeFrom(r)
	if !authed {
		fail(w, http.StatusUnauthorized, "探针令牌无效")
		return
	}
	var in heartbeatIn
	_ = decode(r, &in)
	_ = s.st.TouchProbe(p.ID, s.clientIP(r), strings.TrimSpace(in.Version))
	ok(w, map[string]any{
		"probeId":     p.ID,
		"name":        p.Name,
		"line":        p.Line,
		"intervalSec": 5,
		"serverTime":  time.Now().Unix(),
	})
}

func (s *Server) handleProbeTasks(w http.ResponseWriter, r *http.Request) {
	p, authed := s.probeFrom(r)
	if !authed {
		fail(w, http.StatusUnauthorized, "探针令牌无效")
		return
	}
	_ = s.st.TouchProbe(p.ID, s.clientIP(r), "")

	task, err := s.st.ClaimProbeTask(p.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "取任务失败")
		return
	}
	if task == nil {
		ok(w, map[string]any{"task": nil})
		return
	}

	var payload service.ProbePayload
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
		_ = s.st.FinishProbeTask(task.ID, "failed", "任务内容损坏")
		ok(w, map[string]any{"task": nil})
		return
	}
	ok(w, map[string]any{"task": map[string]any{
		"id":      task.ID,
		"host":    payload.Host,
		"options": payload.Options,
	}})
}

func (s *Server) handleProbeResult(w http.ResponseWriter, r *http.Request) {
	p, authed := s.probeFrom(r)
	if !authed {
		fail(w, http.StatusUnauthorized, "探针令牌无效")
		return
	}
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	var rep service.ProbeReport
	if err := decode(r, &rep); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	pushed, err := s.svc.SubmitProbeResult(r.Context(), p.ID, id, rep)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, map[string]any{"ok": true, "pushed": pushed})
}
