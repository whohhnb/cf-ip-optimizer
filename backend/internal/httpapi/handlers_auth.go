package httpapi

import (
	"net/http"
	"strings"
	"time"

	"edgepilot/internal/auth"
	"edgepilot/internal/logx"
	"edgepilot/internal/model"
)

const tokenTTL = 7 * 24 * time.Hour

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func validEmail(s string) bool {
	if len(s) < 5 || len(s) > 120 || !strings.Contains(s, "@") {
		return false
	}
	parts := strings.Split(s, "@")
	return len(parts) == 2 && parts[1] != "" && !strings.ContainsAny(s, " \t\r\n")
}

func (s *Server) registrationAllowed() bool {
	n, _ := s.st.CountUsers()
	if n == 0 {
		return true
	}
	return s.st.GetSetting("allow_registration", "false") == "true"
}

func (s *Server) handlePublicConfig(w http.ResponseWriter, r *http.Request) {
	n, _ := s.st.CountUsers()
	ok(w, map[string]any{
		"allowRegistration": s.registrationAllowed(),
		"needsSetup":        n == 0,
		"version":           Version,
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.registrationAllowed() {
		fail(w, http.StatusForbidden, "注册已关闭，请联系管理员开通账号")
		return
	}
	var in credentials
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !validEmail(in.Email) {
		fail(w, http.StatusBadRequest, "请输入有效的邮箱地址")
		return
	}
	if len(in.Password) < 8 {
		fail(w, http.StatusBadRequest, "密码至少 8 位")
		return
	}
	if _, err := s.st.UserByEmail(in.Email); err == nil {
		fail(w, http.StatusConflict, "该邮箱已注册")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		fail(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	n, _ := s.st.CountUsers()
	role := "user"
	if n == 0 {
		role = "admin"
	}
	u, err := s.st.CreateUser(in.Email, hash, role)
	if err != nil {
		fail(w, http.StatusInternalServerError, "创建账号失败")
		return
	}
	logx.Logf(u.ID, 0, "info", "账号注册成功 · %s · IP %s · 角色 %s", u.Email, clientIP(r), u.Role)
	token, _ := auth.Issue(s.cfg.Secret, u.ID, u.Email, u.Role, tokenTTL)
	s.setCookie(w, token)
	ok(w, map[string]any{"token": token, "user": u})
}

func (s *Server) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "pilot_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.cfg.BaseURL, "https://"),
		MaxAge:   int(tokenTTL.Seconds()),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	key := in.Email + "|" + clientIP(r)
	if !s.allowAttempt(key) {
		logx.Consolef(logx.Warn, "登录被限流 · %s · IP %s", in.Email, clientIP(r))
		fail(w, http.StatusTooManyRequests, "尝试过于频繁，请稍后再试")
		return
	}

	u, err := s.st.UserByEmail(in.Email)
	if err != nil || !auth.CheckPassword(u.PasswordHash, in.Password) {
		s.noteAttempt(key)
		// console only: failed logins must not leak emails across tenants
		logx.Consolef(logx.Warn, "登录失败 · %s · IP %s", in.Email, clientIP(r))
		fail(w, http.StatusUnauthorized, "邮箱或密码错误")
		return
	}
	logx.Logf(u.ID, 0, logx.Info, "登录成功 · %s · IP %s", u.Email, clientIP(r))
	token, _ := auth.Issue(s.cfg.Secret, u.ID, u.Email, u.Role, tokenTTL)
	s.setCookie(w, token)
	ok(w, map[string]any{"token": token, "user": u})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "pilot_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	ok(w, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	ok(w, map[string]any{"user": u, "allowRegistration": s.registrationAllowed()})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		return host[:i]
	}
	return host
}

// allowAttempt implements a tiny sliding-window limiter (10 tries / 10 min).
func (s *Server) allowAttempt(key string) bool {
	s.limiterMu.Lock()
	defer s.limiterMu.Unlock()
	cutoff := time.Now().Add(-10 * time.Minute)
	list := s.attempts[key]
	kept := list[:0]
	for _, t := range list {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	s.attempts[key] = kept
	return len(kept) < 10
}

func (s *Server) noteAttempt(key string) {
	s.limiterMu.Lock()
	defer s.limiterMu.Unlock()
	s.attempts[key] = append(s.attempts[key], time.Now())
	if len(s.attempts) > 5000 {
		s.attempts = make(map[string][]time.Time)
	}
}

var _ = model.User{}
