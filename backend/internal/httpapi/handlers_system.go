package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"edgepilot/internal/dnsprovider"
	"edgepilot/internal/logx"
)

// Version is overridden at build time with -ldflags "-X ...Version=vX.Y.Z"
// so release binaries report the tag they were cut from.
var Version = "1.1.0"

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"status": "ok", "version": Version, "time": nowISO()})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	o, err := s.st.Overview(u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取概览失败")
		return
	}
	ok(w, o)
}

func (s *Server) handleTrend(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	domainID, _ := strconv.ParseInt(r.URL.Query().Get("domainId"), 10, 64)
	points, err := s.st.LatencyTrend(u.ID, domainID, 40)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取趋势失败")
		return
	}
	ok(w, map[string]any{"points": points})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	level := strings.TrimSpace(r.URL.Query().Get("level"))
	logs, err := s.st.ListLogs(u.ID, level, limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取日志失败")
		return
	}
	ok(w, map[string]any{"logs": logs})
}

var settingDefaults = map[string]string{
	"allow_registration":    "false",
	"allow_private_webhook": "false",
	"rolling_replace":       "true",
	"scan_candidates":       "300",
	"scan_max_concurrent":   "3",
	"scan_concurrency":      "48",
	"scan_timeout_ms":       "1200",
	"scan_samples":          "3",
	"scan_speed_top":        "12",
	"scan_speed_bytes":      "3145728",
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	out := make(map[string]string, len(settingDefaults))
	for k, def := range settingDefaults {
		out[k] = s.st.GetSetting(k, def)
	}
	ok(w, map[string]any{"settings": out, "defaults": settingDefaults})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	bounds := map[string][2]int{
		"scan_candidates":     {20, 3000},
		"scan_max_concurrent": {1, 16},
		"scan_concurrency":    {1, 256},
		"scan_timeout_ms":     {200, 10000},
		"scan_samples":        {1, 8},
		"scan_speed_top":      {1, 60},
		"scan_speed_bytes":    {65536, 52428800},
	}
	changed := make([]string, 0, len(in))
	for k, v := range in {
		if _, known := settingDefaults[k]; !known {
			fail(w, http.StatusBadRequest, "未知的设置项: "+k)
			return
		}
		v = strings.TrimSpace(v)
		switch k {
		case "allow_registration", "allow_private_webhook", "rolling_replace":
			if v != "true" && v != "false" {
				fail(w, http.StatusBadRequest, k+" 只能是 true 或 false")
				return
			}
		default:
			n, err := strconv.Atoi(v)
			if err != nil {
				fail(w, http.StatusBadRequest, k+" 必须是整数")
				return
			}
			if b, exists := bounds[k]; exists && (n < b[0] || n > b[1]) {
				fail(w, http.StatusBadRequest, k+" 超出允许范围")
				return
			}
		}
		if err := s.st.SetSetting(k, v); err != nil {
			fail(w, http.StatusInternalServerError, "保存失败")
			return
		}
		changed = append(changed, k+"="+v)
	}
	// 策略改了要立刻生效，不能等重启
	dnsprovider.SetAllowPrivateOutbound(s.st.GetSetting("allow_private_webhook", "false") == "true")
	s.svc.ApplyRuntimeSettings()
	u := userFrom(r)
	logx.Infof(u.ID, 0, "更新系统设置 · %s", strings.Join(changed, ", "))
	s.handleGetSettings(w, r)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListUsers()
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取用户失败")
		return
	}
	ok(w, map[string]any{"users": users})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	if id == u.ID {
		fail(w, http.StatusBadRequest, "不能删除自己")
		return
	}
	if err := s.st.DeleteUser(id); err != nil {
		fail(w, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func nowISO() string {
	return time.Now().Format(time.RFC3339)
}
