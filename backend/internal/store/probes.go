package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"edgepilot/internal/model"
)

// ProbeOnlineWindow is how long a probe may stay silent before it is shown as
// offline. The agent heartbeats every 30s, so three missed beats is generous.
const ProbeOnlineWindow = 90 * time.Second

const probeCols = `id,user_id,name,line,concurrency,token_hint,version,last_ip,last_seen_at,created_at`

func scanProbe(sc scanner) (*model.Probe, error) {
	var p model.Probe
	var seen, created int64
	if err := sc.Scan(&p.ID, &p.UserID, &p.Name, &p.Line, &p.Concurrency, &p.TokenHint, &p.Version, &p.LastIP, &seen, &created); err != nil {
		return nil, err
	}
	if seen > 0 {
		t := time.Unix(seen, 0)
		p.LastSeenAt = &t
		p.Online = time.Since(t) < ProbeOnlineWindow
	}
	p.CreatedAt = time.Unix(created, 0)
	return &p, nil
}

func (s *Store) CreateProbe(userID int64, name, line string, concurrency int, tokenHash, hint string) (*model.Probe, error) {
	nowT := now()
	res, err := s.db.Exec(`INSERT INTO probes(user_id,name,line,concurrency,token_hash,token_hint,created_at) VALUES(?,?,?,?,?,?,?)`,
		userID, name, line, concurrency, tokenHash, hint, nowT)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.ProbeByID(id, userID)
}

func (s *Store) ListProbes(userID int64) ([]model.Probe, error) {
	rows, err := s.db.Query(`SELECT `+probeCols+` FROM probes WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Probe{}
	for rows.Next() {
		p, err := scanProbe(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) ProbeByID(id, userID int64) (*model.Probe, error) {
	row := s.db.QueryRow(`SELECT `+probeCols+` FROM probes WHERE id=? AND user_id=?`, id, userID)
	p, err := scanProbe(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// ProbeByTokenHash is the authentication lookup: the plaintext token is never
// stored, only its SHA-256 digest.
func (s *Store) ProbeByTokenHash(hash string) (*model.Probe, error) {
	row := s.db.QueryRow(`SELECT `+probeCols+` FROM probes WHERE token_hash=?`, hash)
	p, err := scanProbe(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// ErrProbeInUse is returned when a probe still has domains bound to it.
var ErrProbeInUse = errors.New("probe still bound to domains")

// UpdateProbe edits the descriptive fields. The token is immutable: rotating it
// would silently break a running agent, so the user creates a new probe instead.
func (s *Store) UpdateProbe(id, userID int64, name, line string, concurrency int) error {
	res, err := s.db.Exec(`UPDATE probes SET name=?, line=?, concurrency=? WHERE id=? AND user_id=?`,
		name, line, concurrency, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteProbe(id, userID int64) error {
	// 用一条语句同时完成「还有域名绑着就不删」，避免先数后删之间
	// 有人新建域名绑定上来
	res, err := s.db.Exec(`DELETE FROM probes WHERE id=? AND user_id=?
		AND NOT EXISTS (SELECT 1 FROM domains WHERE probe_id=?)`, id, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if cnt, _ := s.CountDomainsByProbe(id); cnt > 0 {
			return ErrProbeInUse
		}
		return ErrNotFound
	}
	return nil
}

// TouchProbe refreshes liveness. An empty version means "the caller did not
// report one" (task polling), which must not blank what heartbeat recorded.
func (s *Store) TouchProbe(id int64, ip, version string) error {
	if strings.TrimSpace(version) == "" {
		_, err := s.db.Exec(`UPDATE probes SET last_seen_at=?, last_ip=? WHERE id=?`, now(), ip, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE probes SET last_seen_at=?, last_ip=?, version=? WHERE id=?`,
		now(), ip, version, id)
	return err
}

func (s *Store) CreateProbeTask(probeID, domainID, jobID int64, payload string) (*model.ProbeTask, error) {
	nowT := now()
	res, err := s.db.Exec(`INSERT INTO probe_tasks(probe_id,domain_id,job_id,payload,created_at) VALUES(?,?,?,?,?)`,
		probeID, domainID, jobID, payload, nowT)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.ProbeTask{
		ID: id, ProbeID: probeID, DomainID: domainID, JobID: jobID,
		Status: "pending", Payload: payload, CreatedAt: time.Unix(nowT, 0),
	}, nil
}

// ClaimProbeTask hands the oldest pending task to a probe and marks it running
// in the same transaction, so two concurrent polls can never get the same row.
func (s *Store) ClaimProbeTask(probeID int64) (*model.ProbeTask, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var t model.ProbeTask
	var created int64
	err = tx.QueryRow(`SELECT id,probe_id,domain_id,job_id,payload,created_at
		FROM probe_tasks WHERE probe_id=? AND status='pending' ORDER BY id LIMIT 1`, probeID).
		Scan(&t.ID, &t.ProbeID, &t.DomainID, &t.JobID, &t.Payload, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	nowT := now()
	res, err := tx.Exec(`UPDATE probe_tasks SET status='running', started_at=? WHERE id=? AND status='pending'`, nowT, t.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// someone else grabbed it between SELECT and UPDATE
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	t.Status = "running"
	t.CreatedAt = time.Unix(created, 0)
	started := time.Unix(nowT, 0)
	t.StartedAt = &started
	return &t, nil
}

func (s *Store) ProbeTaskByID(id int64) (*model.ProbeTask, error) {
	row := s.db.QueryRow(`SELECT id,probe_id,domain_id,job_id,status,payload,error,created_at,started_at,finished_at
		FROM probe_tasks WHERE id=?`, id)
	var t model.ProbeTask
	var created, started, finished int64
	if err := row.Scan(&t.ID, &t.ProbeID, &t.DomainID, &t.JobID, &t.Status, &t.Payload, &t.Error,
		&created, &started, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.CreatedAt = time.Unix(created, 0)
	if started > 0 {
		v := time.Unix(started, 0)
		t.StartedAt = &v
	}
	if finished > 0 {
		v := time.Unix(finished, 0)
		t.FinishedAt = &v
	}
	return &t, nil
}

// SaveProbeTaskResults keeps one agent's raw output for the later merge.
func (s *Store) SaveProbeTaskResults(id int64, results string, tested int) error {
	_, err := s.db.Exec(`UPDATE probe_tasks SET results=?, tested=?, status='done', finished_at=? WHERE id=?`,
		results, tested, now(), id)
	return err
}

// ProbeTasksByJob returns every task of one scan, with the stored results, so
// the caller can decide whether the job is ready to merge.
func (s *Store) ProbeTasksByJob(jobID int64) ([]model.ProbeTask, error) {
	rows, err := s.db.Query(`SELECT id,probe_id,domain_id,job_id,status,results,tested,error
		FROM probe_tasks WHERE job_id=? ORDER BY id`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ProbeTask
	for rows.Next() {
		var t model.ProbeTask
		if err := rows.Scan(&t.ID, &t.ProbeID, &t.DomainID, &t.JobID, &t.Status, &t.Results, &t.Tested, &t.Error); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) FinishProbeTask(id int64, status, errMsg string) error {
	_, err := s.db.Exec(`UPDATE probe_tasks SET status=?, error=?, finished_at=? WHERE id=?`,
		status, errMsg, now(), id)
	return err
}

// ExpireProbeTasks fails tasks that no probe picked up (or picked up and then
// died on). Without this a single crashed agent would block that domain's slot
// forever, because "running" is only cleared when a result arrives.
func (s *Store) ExpireProbeTasks(pendingBefore, runningBefore int64) ([]model.ProbeTask, error) {
	rows, err := s.db.Query(`SELECT id,probe_id,domain_id,job_id,status FROM probe_tasks
		WHERE (status='pending' AND created_at < ?) OR (status='running' AND started_at < ?)`,
		pendingBefore, runningBefore)
	if err != nil {
		return nil, err
	}
	var expired []model.ProbeTask
	for rows.Next() {
		var t model.ProbeTask
		if err := rows.Scan(&t.ID, &t.ProbeID, &t.DomainID, &t.JobID, &t.Status); err != nil {
			rows.Close()
			return nil, err
		}
		// 两种超时的原因完全不同：一个是探针根本没上线领任务，
		// 一个是领了但跑挂了，排错方向不一样
		if t.Status == "pending" {
			t.Error = "探针未领取任务（可能离线或令牌失效）"
		} else {
			t.Error = "探针执行超时，未回传结果"
		}
		expired = append(expired, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	nowT := now()
	for _, t := range expired {
		_, _ = s.db.Exec(`UPDATE probe_tasks SET status='failed', error=?, finished_at=? WHERE id=?`,
			t.Error, nowT, t.ID)
	}
	return expired, nil
}

// PendingTaskCount tells the dispatcher whether a probe already has work queued.
func (s *Store) PendingTaskCount(probeID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM probe_tasks WHERE probe_id=? AND status IN ('pending','running')`, probeID).Scan(&n)
	return n, err
}
