package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"edgepilot/internal/model"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

var ErrNotFound = errors.New("not found")

func Open(path string) (*Store, error) {
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(8000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One writer connection keeps SQLite happy under the scanner's write bursts.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL DEFAULT 'user',
  token_version INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS dns_accounts (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  provider    TEXT NOT NULL DEFAULT 'dnspod',
  meta        TEXT NOT NULL DEFAULT '',
  credentials TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS domains (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id   INTEGER NOT NULL REFERENCES dns_accounts(id) ON DELETE CASCADE,
  root_domain  TEXT NOT NULL,
  sub_domain   TEXT NOT NULL DEFAULT '@',
  record_line  TEXT NOT NULL DEFAULT '默认',
  ttl          INTEGER NOT NULL DEFAULT 600,
  slots        INTEGER NOT NULL DEFAULT 2,
  auto_push    INTEGER NOT NULL DEFAULT 1,
  interval_min INTEGER NOT NULL DEFAULT 60,
  enabled      INTEGER NOT NULL DEFAULT 1,
  status       TEXT NOT NULL DEFAULT 'idle',
  status_msg   TEXT NOT NULL DEFAULT '',
  last_scan_at INTEGER,
  last_push_at INTEGER,
  created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS scan_jobs (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  domain_id   INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
  user_id     INTEGER NOT NULL,
  trigger     TEXT NOT NULL,
  status      TEXT NOT NULL,
  candidates  INTEGER NOT NULL DEFAULT 0,
  tested      INTEGER NOT NULL DEFAULT 0,
  ok          INTEGER NOT NULL DEFAULT 0,
  progress    INTEGER NOT NULL DEFAULT 0,
  stage       TEXT NOT NULL DEFAULT '',
  error       TEXT NOT NULL DEFAULT '',
  pushed      INTEGER NOT NULL DEFAULT 0,
  top_ip      TEXT NOT NULL DEFAULT '',
  top_latency REAL NOT NULL DEFAULT 0,
  top_speed   REAL NOT NULL DEFAULT 0,
  started_at  INTEGER NOT NULL,
  finished_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_jobs_domain ON scan_jobs(domain_id, started_at DESC);

CREATE TABLE IF NOT EXISTS scan_results (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id  INTEGER NOT NULL REFERENCES scan_jobs(id) ON DELETE CASCADE,
  ip      TEXT NOT NULL,
  latency REAL NOT NULL DEFAULT 0,
  jitter  REAL NOT NULL DEFAULT 0,
  loss    REAL NOT NULL DEFAULT 0,
  speed   REAL NOT NULL DEFAULT 0,
  score   REAL NOT NULL DEFAULT 0,
  colo    TEXT NOT NULL DEFAULT '',
  ok      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_results_job ON scan_results(job_id, score DESC);

CREATE TABLE IF NOT EXISTS managed_records (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  domain_id  INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
  record_id  TEXT NOT NULL,
  slot       INTEGER NOT NULL,
  value      TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(domain_id, slot)
);

CREATE TABLE IF NOT EXISTS logs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL DEFAULT 0,
  domain_id  INTEGER NOT NULL DEFAULT 0,
  level      TEXT NOT NULL DEFAULT 'info',
  message    TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_logs_time ON logs(created_at DESC);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	// Older builds stored a fixed secretId/secretKey pair. Bring such tables up
	// to the generic credential columns so the multi-provider code can run.
	cols, err := s.columns("dns_accounts")
	if err != nil {
		return err
	}
	// 老库补 token_version 列（新增于 v1.2 的改密踢下线）
	ucols, err := s.columns("users")
	if err != nil {
		return err
	}
	if !ucols["token_version"] {
		if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN token_version INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("migrate users: %w", err)
		}
	}
	if !cols["credentials"] {
		for _, stmt := range []string{
			`ALTER TABLE dns_accounts ADD COLUMN meta TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE dns_accounts ADD COLUMN credentials TEXT NOT NULL DEFAULT ''`,
		} {
			if _, err := s.db.Exec(stmt); err != nil {
				return fmt.Errorf("migrate accounts: %w", err)
			}
		}
	}
	return nil
}

func (s *Store) columns(table string) (map[string]bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func now() int64 { return time.Now().Unix() }

func tsPtr(v sql.NullInt64) *time.Time {
	if !v.Valid || v.Int64 == 0 {
		return nil
	}
	t := time.Unix(v.Int64, 0)
	return &t
}

func ts(t time.Time) int64 { return t.Unix() }

// ---------- users ----------

func (s *Store) CreateUser(email, hash, role string) (*model.User, error) {
	nowT := now()
	res, err := s.db.Exec(`INSERT INTO users(email,password_hash,role,created_at) VALUES(?,?,?,?)`,
		email, hash, role, nowT)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.User{ID: id, Email: email, PasswordHash: hash, Role: role, CreatedAt: time.Unix(nowT, 0)}, nil
}

// CreateUserAutoRole 在一条 INSERT 里决定角色：库里没有任何用户时成为管理员。
// 分两步「先数再插」的话，并发注册会让两个请求同时看到 0 个用户，双双拿到管理员。
func (s *Store) CreateUserAutoRole(email, hash string) (*model.User, error) {
	nowT := now()
	res, err := s.db.Exec(`INSERT INTO users(email,password_hash,role,created_at)
		VALUES(?,?, CASE WHEN (SELECT COUNT(*) FROM users)=0 THEN 'admin' ELSE 'user' END, ?)`,
		email, hash, nowT)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.UserByID(id)
}

func (s *Store) UserByEmail(email string) (*model.User, error) {
	row := s.db.QueryRow(`SELECT id,email,password_hash,role,token_version,created_at FROM users WHERE email=?`, email)
	var u model.User
	var created int64
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.TokenVersion, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.CreatedAt = time.Unix(created, 0)
	return &u, nil
}

func (s *Store) UserByID(id int64) (*model.User, error) {
	row := s.db.QueryRow(`SELECT id,email,password_hash,role,token_version,created_at FROM users WHERE id=?`, id)
	var u model.User
	var created int64
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.TokenVersion, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.CreatedAt = time.Unix(created, 0)
	return &u, nil
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) ListUsers() ([]model.User, error) {
	rows, err := s.db.Query(`SELECT id,email,role,created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.User{}
	for rows.Next() {
		var u model.User
		var created int64
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &created); err != nil {
			return nil, err
		}
		u.CreatedAt = time.Unix(created, 0)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *Store) SetPassword(userID int64, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, userID)
	return err
}

// BumpTokenVersion 让该用户所有已签发的 JWT 立即失效，返回新的版本号
func (s *Store) BumpTokenVersion(userID int64) (int64, error) {
	if _, err := s.db.Exec(`UPDATE users SET token_version = token_version + 1 WHERE id=?`, userID); err != nil {
		return 0, err
	}
	var v int64
	if err := s.db.QueryRow(`SELECT token_version FROM users WHERE id=?`, userID).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// ---------- dns accounts ----------

func (s *Store) CreateAccount(userID int64, name, provider, meta, credEnc string) (*model.DNSAccount, error) {
	nowT := now()
	res, err := s.db.Exec(`INSERT INTO dns_accounts(user_id,name,provider,meta,credentials,created_at) VALUES(?,?,?,?,?,?)`,
		userID, name, provider, meta, credEnc, nowT)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.DNSAccount{ID: id, UserID: userID, Name: name, Provider: provider, Meta: meta, CreatedAt: time.Unix(nowT, 0)}, nil
}

func (s *Store) ListAccounts(userID int64) ([]model.DNSAccount, error) {
	rows, err := s.db.Query(`SELECT id,user_id,name,provider,meta,created_at FROM dns_accounts WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DNSAccount{}
	for rows.Next() {
		var a model.DNSAccount
		var created int64
		if err := rows.Scan(&a.ID, &a.UserID, &a.Name, &a.Provider, &a.Meta, &created); err != nil {
			return nil, err
		}
		a.CreatedAt = time.Unix(created, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

// AccountSecret returns the account plus its still-encrypted credential blob.
func (s *Store) AccountSecret(id int64) (*model.DNSAccount, string, error) {
	row := s.db.QueryRow(`SELECT id,user_id,name,provider,meta,credentials,created_at FROM dns_accounts WHERE id=?`, id)
	var a model.DNSAccount
	var enc string
	var created int64
	if err := row.Scan(&a.ID, &a.UserID, &a.Name, &a.Provider, &a.Meta, &enc, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	a.CreatedAt = time.Unix(created, 0)
	return &a, enc, nil
}

// UpdateAccount rewrites name/provider/credentials in place so users can rotate
// keys without deleting the account (which would cascade to their domains).
func (s *Store) UpdateAccount(id, userID int64, name, provider, meta, credEnc string) error {
	res, err := s.db.Exec(`UPDATE dns_accounts SET name=?,provider=?,meta=?,credentials=? WHERE id=? AND user_id=?`,
		name, provider, meta, credEnc, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAccount(id, userID int64) error {
	_, err := s.db.Exec(`DELETE FROM dns_accounts WHERE id=? AND user_id=?`, id, userID)
	return err
}

// ---------- settings ----------

func (s *Store) GetSetting(key, def string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
