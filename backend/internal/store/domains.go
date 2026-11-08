package store

import (
	"database/sql"
	"errors"
	"time"

	"edgepilot/internal/model"
)

const domainCols = `id,user_id,account_id,root_domain,sub_domain,record_line,colo_allow,probe_id,ttl,slots,auto_push,interval_min,enabled,status,status_msg,last_scan_at,last_push_at,created_at`

type scanner interface{ Scan(dest ...any) error }

func scanDomain(sc scanner) (*model.Domain, error) {
	var d model.Domain
	var autoPush, enabled int
	var lastScan, lastPush sql.NullInt64
	var created int64
	if err := sc.Scan(&d.ID, &d.UserID, &d.AccountID, &d.RootDomain, &d.SubDomain, &d.RecordLine, &d.ColoAllow, &d.ProbeID,
		&d.TTL, &d.Slots, &autoPush, &d.IntervalMin, &enabled, &d.Status, &d.StatusMsg,
		&lastScan, &lastPush, &created); err != nil {
		return nil, err
	}
	d.AutoPush = autoPush != 0
	d.Enabled = enabled != 0
	d.LastScanAt = tsPtr(lastScan)
	d.LastPushAt = tsPtr(lastPush)
	d.CreatedAt = time.Unix(created, 0)
	return &d, nil
}

type DomainInput struct {
	AccountID   int64
	RootDomain  string
	SubDomain   string
	RecordLine  string
	ColoAllow   string
	ProbeIDs    []int64
	TTL         int
	Slots       int
	AutoPush    bool
	IntervalMin int
	Enabled     bool
}

func (s *Store) CreateDomain(userID int64, in DomainInput) (*model.Domain, error) {
	nowT := now()
	primary := int64(0)
	if len(in.ProbeIDs) > 0 {
		primary = in.ProbeIDs[0]
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO domains(user_id,account_id,root_domain,sub_domain,record_line,colo_allow,probe_id,ttl,slots,auto_push,interval_min,enabled,status,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'idle',?)`,
		userID, in.AccountID, in.RootDomain, in.SubDomain, in.RecordLine, in.ColoAllow, primary, in.TTL, in.Slots,
		boolInt(in.AutoPush), in.IntervalMin, boolInt(in.Enabled), nowT)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	for _, pid := range in.ProbeIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO domain_probes(domain_id,probe_id) VALUES(?,?)`, id, pid); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.DomainByID(id, userID)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) ListDomains(userID int64) ([]model.Domain, error) {
	rows, err := s.db.Query(`SELECT `+domainCols+` FROM domains WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachProbes(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) AllDomains() ([]model.Domain, error) {
	rows, err := s.db.Query(`SELECT ` + domainCols + ` FROM domains ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachProbes(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) DomainByID(id, userID int64) (*model.Domain, error) {
	row := s.db.QueryRow(`SELECT `+domainCols+` FROM domains WHERE id=? AND user_id=?`, id, userID)
	d, err := scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if ids, err := s.DomainProbeIDs(d.ID); err == nil {
		d.ProbeIDs = ids
	}
	return d, nil
}

// DomainConflict reports whether this user already has a config for the same
// host and the same record line. Two configs on one line would both think they
// own the same record set and overwrite each other on every scan.
func (s *Store) DomainConflict(userID int64, root, sub, line string, excludeID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM domains
		WHERE user_id=? AND root_domain=? AND sub_domain=? AND record_line=? AND id<>?`,
		userID, root, sub, line, excludeID).Scan(&n)
	return n > 0, err
}

// DomainByIDInternal fetches a domain without an ownership filter. Only the
// probe result path uses it: the agent authenticates as itself, and the domain
// it scanned was already authorized when the task was queued.
func (s *Store) DomainByIDInternal(id int64) (*model.Domain, error) {
	row := s.db.QueryRow(`SELECT `+domainCols+` FROM domains WHERE id=?`, id)
	d, err := scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if ids, err := s.DomainProbeIDs(d.ID); err == nil {
		d.ProbeIDs = ids
	}
	return d, nil
}

// CountDomainsByProbe guards probe deletion: dropping a probe that domains
// still point at would leave them unable to scan with no obvious cause.
func (s *Store) CountDomainsByProbe(probeID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM domains WHERE probe_id=?`, probeID).Scan(&n)
	return n, err
}

func (s *Store) UpdateDomain(id, userID int64, in DomainInput) error {
	primary := int64(0)
	if len(in.ProbeIDs) > 0 {
		primary = in.ProbeIDs[0]
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE domains SET account_id=?,root_domain=?,sub_domain=?,record_line=?,colo_allow=?,probe_id=?,ttl=?,slots=?,auto_push=?,interval_min=?,enabled=? WHERE id=? AND user_id=?`,
		in.AccountID, in.RootDomain, in.SubDomain, in.RecordLine, in.ColoAllow, primary, in.TTL, in.Slots,
		boolInt(in.AutoPush), in.IntervalMin, boolInt(in.Enabled), id, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM domain_probes WHERE domain_id=?`, id); err != nil {
		return err
	}
	for _, pid := range in.ProbeIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO domain_probes(domain_id,probe_id) VALUES(?,?)`, id, pid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteDomain(id, userID int64) error {
	_, err := s.db.Exec(`DELETE FROM domains WHERE id=? AND user_id=?`, id, userID)
	return err
}

func (s *Store) SetDomainStatus(id int64, status, msg string) error {
	_, err := s.db.Exec(`UPDATE domains SET status=?,status_msg=? WHERE id=?`, status, msg, id)
	return err
}

func (s *Store) TouchDomainScan(id int64) error {
	_, err := s.db.Exec(`UPDATE domains SET last_scan_at=? WHERE id=?`, now(), id)
	return err
}

func (s *Store) TouchDomainPush(id int64) error {
	_, err := s.db.Exec(`UPDATE domains SET last_push_at=? WHERE id=?`, now(), id)
	return err
}

// ---------- scan jobs ----------

func (s *Store) CreateJob(domainID, userID int64, trigger string, candidates int) (*model.ScanJob, error) {
	nowT := now()
	res, err := s.db.Exec(`INSERT INTO scan_jobs(domain_id,user_id,trigger,status,candidates,stage,started_at) VALUES(?,?,?,'running',?,'采样中',?)`,
		domainID, userID, trigger, candidates, nowT)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.JobByID(id)
}

func (s *Store) UpdateJobProgress(id int64, tested, ok, progress int, stage string) error {
	_, err := s.db.Exec(`UPDATE scan_jobs SET tested=?,ok=?,progress=?,stage=? WHERE id=?`, tested, ok, progress, stage, id)
	return err
}

func (s *Store) FinishJob(id int64, status, errMsg string, topIP string, topLatency, topSpeed float64, pushed bool) error {
	_, err := s.db.Exec(`UPDATE scan_jobs SET status=?,error=?,top_ip=?,top_latency=?,top_speed=?,pushed=?,progress=100,finished_at=? WHERE id=?`,
		status, errMsg, topIP, topLatency, topSpeed, boolInt(pushed), now(), id)
	return err
}

func (s *Store) JobByID(id int64) (*model.ScanJob, error) {
	row := s.db.QueryRow(`SELECT id,domain_id,user_id,trigger,status,candidates,tested,ok,progress,stage,error,pushed,top_ip,top_latency,top_speed,started_at,finished_at FROM scan_jobs WHERE id=?`, id)
	return scanJob(row)
}

func scanJob(sc scanner) (*model.ScanJob, error) {
	var j model.ScanJob
	var pushed int
	var started int64
	var finished sql.NullInt64
	if err := sc.Scan(&j.ID, &j.DomainID, &j.UserID, &j.Trigger, &j.Status, &j.Candidates, &j.Tested, &j.OK,
		&j.Progress, &j.Stage, &j.Error, &pushed, &j.TopIP, &j.TopLatency, &j.TopSpeed, &started, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	j.Pushed = pushed != 0
	j.StartedAt = time.Unix(started, 0)
	j.FinishedAt = tsPtr(finished)
	return &j, nil
}

func (s *Store) ListJobs(domainID, userID int64, limit int) ([]model.ScanJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT id,domain_id,user_id,trigger,status,candidates,tested,ok,progress,stage,error,pushed,top_ip,top_latency,top_speed,started_at,finished_at
		FROM scan_jobs WHERE domain_id=? AND user_id=? ORDER BY id DESC LIMIT ?`, domainID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ScanJob{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (s *Store) LatestDoneJob(domainID int64) (*model.ScanJob, error) {
	row := s.db.QueryRow(`SELECT id,domain_id,user_id,trigger,status,candidates,tested,ok,progress,stage,error,pushed,top_ip,top_latency,top_speed,started_at,finished_at
		FROM scan_jobs WHERE domain_id=? AND status='done' ORDER BY id DESC LIMIT 1`, domainID)
	return scanJob(row)
}

// ---------- scan results ----------

func (s *Store) InsertResults(jobID int64, results []model.ScanResult) error {
	if len(results) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO scan_results(job_id,ip,latency,jitter,loss,http_rtt,speed,score,colo,ok) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, r := range results {
		if _, err := stmt.Exec(jobID, r.IP, r.Latency, r.Jitter, r.Loss, r.HTTPRTT, r.Speed, r.Score, r.Colo, boolInt(r.OK)); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SeedIPs returns the best addresses from this domain's most recent finished
// scan. Feeding them back as candidates beats another round of pure random
// sampling, because anycast sends most new samples to the same PoP anyway.
func (s *Store) SeedIPs(domainID int64, n int) []string {
	if n <= 0 {
		return nil
	}
	var jobID int64
	err := s.db.QueryRow(`SELECT id FROM scan_jobs WHERE domain_id=? AND status='done'
		ORDER BY COALESCE(finished_at, started_at) DESC LIMIT 1`, domainID).Scan(&jobID)
	if err != nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT ip FROM scan_results WHERE job_id=? AND ok=1 AND score>0
		ORDER BY score DESC LIMIT ?`, jobID, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err == nil {
			out = append(out, ip)
		}
	}
	return out
}

func (s *Store) ResultsByJob(jobID int64, limit int) ([]model.ScanResult, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id,job_id,ip,latency,jitter,loss,http_rtt,speed,score,colo,ok FROM scan_results WHERE job_id=? ORDER BY ok DESC, score DESC LIMIT ?`, jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ScanResult{}
	for rows.Next() {
		var r model.ScanResult
		var ok int
		if err := rows.Scan(&r.ID, &r.JobID, &r.IP, &r.Latency, &r.Jitter, &r.Loss, &r.HTTPRTT, &r.Speed, &r.Score, &r.Colo, &ok); err != nil {
			return nil, err
		}
		r.OK = ok != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- managed records ----------

// ListManagedForUser loads every managed record of one user in a single query.
// The domain list shows the current IPs per line, and doing it per row would be
// an N+1 over SQLite's single writer connection.
func (s *Store) ListManagedForUser(userID int64) (map[int64][]model.ManagedRecord, error) {
	rows, err := s.db.Query(`SELECT m.id,m.domain_id,m.record_id,m.slot,m.value,m.updated_at
		FROM managed_records m JOIN domains d ON d.id = m.domain_id
		WHERE d.user_id=? ORDER BY m.domain_id, m.slot`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]model.ManagedRecord{}
	for rows.Next() {
		var m model.ManagedRecord
		var upd int64
		if err := rows.Scan(&m.ID, &m.DomainID, &m.RecordID, &m.Slot, &m.Value, &upd); err != nil {
			return nil, err
		}
		m.UpdatedAt = time.Unix(upd, 0)
		out[m.DomainID] = append(out[m.DomainID], m)
	}
	return out, rows.Err()
}

func (s *Store) ListManaged(domainID int64) ([]model.ManagedRecord, error) {
	rows, err := s.db.Query(`SELECT id,domain_id,record_id,slot,value,updated_at FROM managed_records WHERE domain_id=? ORDER BY slot`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ManagedRecord{}
	for rows.Next() {
		var m model.ManagedRecord
		var upd int64
		if err := rows.Scan(&m.ID, &m.DomainID, &m.RecordID, &m.Slot, &m.Value, &upd); err != nil {
			return nil, err
		}
		m.UpdatedAt = time.Unix(upd, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) UpsertManaged(domainID int64, slot int, recordID, value string) error {
	_, err := s.db.Exec(`INSERT INTO managed_records(domain_id,record_id,slot,value,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(domain_id,slot) DO UPDATE SET record_id=excluded.record_id,value=excluded.value,updated_at=excluded.updated_at`,
		domainID, recordID, slot, value, now())
	return err
}

func (s *Store) DeleteManaged(domainID int64) error {
	_, err := s.db.Exec(`DELETE FROM managed_records WHERE domain_id=?`, domainID)
	return err
}

// ---------- logs ----------

func (s *Store) AddLog(userID, domainID int64, level, message string) {
	_, _ = s.db.Exec(`INSERT INTO logs(user_id,domain_id,level,message,created_at) VALUES(?,?,?,?,?)`,
		userID, domainID, level, message, now())
	// keep the table bounded
	_, _ = s.db.Exec(`DELETE FROM logs WHERE id < (SELECT MAX(id)-5000 FROM logs)`)
}

func (s *Store) ListLogs(userID int64, level string, limit int) ([]model.LogEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := `SELECT id,user_id,domain_id,level,message,created_at FROM logs WHERE (user_id=? OR user_id=0)`
	args := []any{userID}
	if level != "" && level != "all" {
		q += ` AND level=?`
		args = append(args, level)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.LogEntry{}
	for rows.Next() {
		var l model.LogEntry
		var created int64
		if err := rows.Scan(&l.ID, &l.UserID, &l.DomainID, &l.Level, &l.Message, &created); err != nil {
			return nil, err
		}
		l.CreatedAt = time.Unix(created, 0)
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------- overview ----------

func (s *Store) Overview(userID int64) (*model.Overview, error) {
	o := &model.Overview{}
	var avgLat, bestLat sql.NullFloat64
	row := s.db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='scanning' THEN 1 ELSE 0 END),0)
		FROM domains WHERE user_id=?`, userID)
	if err := row.Scan(&o.Domains, &o.Healthy, &o.Scanning); err != nil {
		return nil, err
	}
	row = s.db.QueryRow(`SELECT AVG(j.top_latency), MIN(j.top_latency), MAX(j.started_at)
		FROM scan_jobs j
		WHERE j.user_id=? AND j.status='done' AND j.top_latency > 0 AND j.started_at > ?`, userID, now()-7*86400)
	_ = row.Scan(&avgLat, &bestLat, new(sql.NullInt64))
	if avgLat.Valid {
		o.AvgLatency = avgLat.Float64
	}
	if bestLat.Valid {
		o.BestLatency = bestLat.Float64
	}
	var lastStart sql.NullInt64
	row = s.db.QueryRow(`SELECT MAX(started_at) FROM scan_jobs WHERE user_id=?`, userID)
	_ = row.Scan(&lastStart)
	if lastStart.Valid {
		o.LastScanAt = time.Unix(lastStart.Int64, 0).Format(time.RFC3339)
	}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM scan_jobs WHERE user_id=? AND pushed=1 AND finished_at > ?`,
		userID, startOfToday()).Scan(&o.PushesToday)
	return o, nil
}

func startOfToday() int64 {
	t := time.Now()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).Unix()
}

// TrendPoint is a lightweight series entry for the dashboard chart.
type TrendPoint struct {
	T       int64   `json:"t"`
	Latency float64 `json:"latency"`
	Speed   float64 `json:"speed"`
}

func (s *Store) LatencyTrend(userID, domainID int64, limit int) ([]TrendPoint, error) {
	if limit <= 0 {
		limit = 40
	}
	q := `SELECT started_at, top_latency, top_speed FROM scan_jobs
		WHERE user_id=? AND status='done' AND top_latency > 0`
	args := []any{userID}
	if domainID > 0 {
		q += ` AND domain_id=?`
		args = append(args, domainID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrendPoint{}
	for rows.Next() {
		var p TrendPoint
		if err := rows.Scan(&p.T, &p.Latency, &p.Speed); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	// reverse to chronological order
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
