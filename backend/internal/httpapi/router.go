package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"edgepilot/internal/auth"
	"edgepilot/internal/config"
	"edgepilot/internal/logx"
	"edgepilot/internal/model"
	"edgepilot/internal/scanner"
	"edgepilot/internal/service"
	"edgepilot/internal/store"
)

type ctxKey string

const (
	ctxUser    ctxKey = "user"
	ctxClaims  ctxKey = "claims"
	maxBodyLen        = 1 << 20
)

type Server struct {
	cfg   config.Config
	st    *store.Store
	svc   *service.Service
	hub   *scanner.Hub
	webFS fs.FS
	mux   *http.ServeMux

	limiterMu sync.Mutex
	attempts  map[string][]time.Time
}

func New(cfg config.Config, st *store.Store, svc *service.Service, hub *scanner.Hub, webFS fs.FS) *Server {
	s := &Server{
		cfg:      cfg,
		st:       st,
		svc:      svc,
		hub:      hub,
		webFS:    webFS,
		mux:      http.NewServeMux(),
		attempts: make(map[string][]time.Time),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.recoverer(s.securityHeaders(s.logger(s.mux)))
}

func (s *Server) routes() {
	m := s.mux

	m.HandleFunc("GET /api/health", s.handleHealth)
	m.HandleFunc("GET /api/config", s.handlePublicConfig)
	m.HandleFunc("POST /api/auth/register", s.handleRegister)
	m.HandleFunc("POST /api/auth/login", s.handleLogin)
	m.HandleFunc("POST /api/auth/logout", s.handleLogout)
	m.HandleFunc("PUT /api/auth/password", s.withAuth(s.handleChangePassword))
	m.HandleFunc("GET /api/auth/me", s.withAuth(s.handleMe))

	m.HandleFunc("GET /api/overview", s.withAuth(s.handleOverview))
	m.HandleFunc("GET /api/trend", s.withAuth(s.handleTrend))
	m.HandleFunc("GET /api/logs", s.withAuth(s.handleLogs))

	m.HandleFunc("GET /api/providers", s.withAuth(s.handleListProviders))
	m.HandleFunc("GET /api/accounts", s.withAuth(s.handleListAccounts))
	m.HandleFunc("POST /api/accounts", s.withAuth(s.handleCreateAccount))
	m.HandleFunc("POST /api/accounts/test", s.withAuth(s.handleTestAccount))
	m.HandleFunc("PUT /api/accounts/{id}", s.withAuth(s.handleUpdateAccount))
	m.HandleFunc("DELETE /api/accounts/{id}", s.withAuth(s.handleDeleteAccount))

	m.HandleFunc("GET /api/domains", s.withAuth(s.handleListDomains))
	m.HandleFunc("POST /api/domains", s.withAuth(s.handleCreateDomain))
	m.HandleFunc("GET /api/domains/{id}", s.withAuth(s.handleGetDomain))
	m.HandleFunc("PUT /api/domains/{id}", s.withAuth(s.handleUpdateDomain))
	m.HandleFunc("DELETE /api/domains/{id}", s.withAuth(s.handleDeleteDomain))
	m.HandleFunc("POST /api/domains/{id}/scan", s.withAuth(s.handleStartScan))
	m.HandleFunc("POST /api/domains/{id}/push", s.withAuth(s.handlePush))
	m.HandleFunc("GET /api/domains/{id}/records", s.withAuth(s.handleLiveRecords))
	m.HandleFunc("GET /api/domains/{id}/jobs", s.withAuth(s.handleListJobs))

	m.HandleFunc("GET /api/jobs/{id}", s.withAuth(s.handleGetJob))
	m.HandleFunc("GET /api/jobs/{id}/results", s.withAuth(s.handleJobResults))
	m.HandleFunc("GET /api/jobs/{id}/export.csv", s.withAuth(s.handleExportCSV))

	m.HandleFunc("GET /api/stream", s.withAuth(s.handleStream))

	m.HandleFunc("GET /api/settings", s.withAdmin(s.handleGetSettings))
	m.HandleFunc("PUT /api/settings", s.withAdmin(s.handleUpdateSettings))
	m.HandleFunc("GET /api/users", s.withAdmin(s.handleListUsers))
	m.HandleFunc("DELETE /api/users/{id}", s.withAdmin(s.handleDeleteUser))

	// static SPA
	s.static()
}

// ---------- middleware ----------

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logx.Errorf(0, 0, "请求处理 panic · %s %s · %v", r.Method, r.URL.Path, rec)
				fail(w, http.StatusInternalServerError, "服务器内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the response code for request logging while keeping
// Flush/Unwrap available so SSE still streams.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		path := r.URL.Path
		if !strings.HasPrefix(path, "/api/") || path == "/api/health" || path == "/api/stream" {
			return
		}
		dur := time.Since(start).Round(time.Millisecond)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		line := "%s %s → %d · %s · %s"
		args := []any{r.Method, path, status, dur, s.clientIP(r)}
		switch {
		case status >= 500:
			logx.Errorf(0, 0, line, args...)
		case status >= 400:
			logx.Warnf(0, 0, line, args...)
		default:
			logx.Consolef(logx.Info, line, args...)
		}
	})
}

func (s *Server) token(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("pilot_token"); err == nil && c.Value != "" {
		return c.Value
	}
	// EventSource cannot set headers, so the SSE endpoint also accepts a query
	// token as a fallback when the cookie is unavailable.
	if strings.HasPrefix(r.URL.Path, "/api/stream") {
		return r.URL.Query().Get("token")
	}
	return ""
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, err := auth.Parse(s.cfg.Secret, s.token(r))
		if err != nil {
			fail(w, http.StatusUnauthorized, "登录已过期，请重新登录")
			return
		}
		u, err := s.st.UserByID(claims.Sub)
		if err != nil {
			fail(w, http.StatusUnauthorized, "账号不存在")
			return
		}
		// token 版本对不上说明改过密码或已登出，旧 token 一律作废
		if claims.Ver != u.TokenVersion {
			fail(w, http.StatusUnauthorized, "登录已失效，请重新登录")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, u)
		ctx = context.WithValue(ctx, ctxClaims, claims)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) withAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		if u == nil || u.Role != "admin" {
			fail(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next(w, r)
	})
}

func userFrom(r *http.Request) *model.User {
	u, _ := r.Context().Value(ctxUser).(*model.User)
	return u
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyLen))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) static() {
	var sub fs.FS
	if s.webFS != nil {
		sub = s.webFS
	}
	if sub == nil {
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "frontend not built", http.StatusNotFound)
		})
		return
	}
	fileServer := http.FileServer(http.FS(sub))

	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			fail(w, http.StatusNotFound, "接口不存在")
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if st, err := fs.Stat(sub, p); err != nil || st.IsDir() {
			// SPA fallback
			w.Header().Set("Cache-Control", "no-cache")
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}
