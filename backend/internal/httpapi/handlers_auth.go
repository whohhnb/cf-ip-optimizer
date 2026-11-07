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
	u, err := s.st.CreateUserAutoRole(in.Email, hash)
	if err != nil {
		fail(w, http.StatusInternalServerError, "创建账号失败")
		return
	}
	logx.Logf(u.ID, 0, "info", "账号注册成功 · %s · IP %s · 角色 %s", u.Email, s.clientIP(r), u.Role)
	token, _ := auth.Issue(s.cfg.Secret, u.ID, u.Email, u.Role, u.TokenVersion, tokenTTL)
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

	key := in.Email + "|" + s.clientIP(r)
	if !s.allowAttempt(key) {
		logx.Consolef(logx.Warn, "登录被限流 · %s · IP %s", in.Email, s.clientIP(r))
		fail(w, http.StatusTooManyRequests, "尝试过于频繁，请稍后再试")
		return
	}

	u, err := s.st.UserByEmail(in.Email)
	if err != nil {
		// 账号不存在也要跑一次同等开销的哈希校验，否则响应时间会泄露
		// 该邮箱是否注册过
		auth.EqualizeTiming(in.Password)
		s.noteAttempt(key)
		logx.Consolef(logx.Warn, "登录失败 · %s · IP %s", in.Email, s.clientIP(r))
		fail(w, http.StatusUnauthorized, "邮箱或密码错误")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Password) {
		s.noteAttempt(key)
		// console only: failed logins must not leak emails across tenants
		logx.Consolef(logx.Warn, "登录失败 · %s · IP %s", in.Email, s.clientIP(r))
		fail(w, http.StatusUnauthorized, "邮箱或密码错误")
		return
	}
	logx.Logf(u.ID, 0, logx.Info, "登录成功 · %s · IP %s", u.Email, s.clientIP(r))
	token, _ := auth.Issue(s.cfg.Secret, u.ID, u.Email, u.Role, u.TokenVersion, tokenTTL)
	s.setCookie(w, token)
	ok(w, map[string]any{"token": token, "user": u})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// 清 Cookie 只是客户端动作，服务端必须把版本号 +1，
	// 否则这个 token 还能继续用满 7 天
	if claims, err := auth.Parse(s.cfg.Secret, s.token(r)); err == nil {
		_, _ = s.st.BumpTokenVersion(claims.Sub)
	}
	http.SetCookie(w, &http.Cookie{Name: "pilot_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	ok(w, map[string]bool{"ok": true})
}

type passwordInput struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// handleChangePassword 改密码后旧 token 全部失效，并直接下发一个新的，
// 免得用户改完密码还要再登录一次
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in passwordInput
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.OldPassword) {
		logx.Consolef(logx.Warn, "修改密码失败（原密码错误）· %s · IP %s", u.Email, s.clientIP(r))
		fail(w, http.StatusBadRequest, "原密码不正确")
		return
	}
	if len(in.NewPassword) < 8 {
		fail(w, http.StatusBadRequest, "新密码至少 8 位")
		return
	}
	if in.NewPassword == in.OldPassword {
		fail(w, http.StatusBadRequest, "新密码不能与原密码相同")
		return
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		fail(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	if err := s.st.SetPassword(u.ID, hash); err != nil {
		fail(w, http.StatusInternalServerError, "保存失败")
		return
	}
	ver, err := s.st.BumpTokenVersion(u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "保存失败")
		return
	}
	token, _ := auth.Issue(s.cfg.Secret, u.ID, u.Email, u.Role, ver, tokenTTL)
	s.setCookie(w, token)
	logx.Logf(u.ID, 0, logx.Info, "修改密码 · %s · 其余设备已下线", u.Email)
	ok(w, map[string]any{"token": token})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	ok(w, map[string]any{"user": u, "allowRegistration": s.registrationAllowed()})
}

// clientIP 只有显式开启 PILOT_TRUST_PROXY 时才采信 X-Forwarded-For。
// 否则攻击者随手伪造该头即可绕开登录限流、并污染审计日志。
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
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
	// 表满时只清理已过期的键，不能整体清空：否则攻击者批量制造 key
	// 就能把所有人的失败计数一起抹掉，限流形同虚设
	if len(s.attempts) > 5000 {
		cutoff := time.Now().Add(-10 * time.Minute)
		for k, v := range s.attempts {
			if len(v) == 0 || v[len(v)-1].Before(cutoff) {
				delete(s.attempts, k)
			}
		}
	}
}

var _ = model.User{}
