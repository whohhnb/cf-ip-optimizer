package store

import (
	"time"

	"edgepilot/internal/model"
)

// 候选前缀的观测记录：只写「最近一次」，不做历史累积。
//
// 它和 candidate_prefixes 是两回事——那张是配置，这张是观测。观测不参与
// 候选生成，只用来回答「上次扫描时这个段还落 NRT 吗、多快」。

// SavePrefixChecks overwrites the last observation for the given prefixes.
func (s *Store) SavePrefixChecks(line string, checks []model.PrefixCheck) error {
	if len(checks) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, c := range checks {
		if _, err := tx.Exec(
			`INSERT INTO candidate_prefix_checks (line, prefix, colo, latency, samples, ok_count, checked_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(line, prefix) DO UPDATE SET
			   colo = excluded.colo,
			   latency = excluded.latency,
			   samples = excluded.samples,
			   ok_count = excluded.ok_count,
			   checked_at = excluded.checked_at`,
			line, c.Prefix, c.Colo, c.Latency, c.Samples, c.OKCount, c.CheckedAt.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListPrefixChecks returns the last observation for every prefix, grouped by line.
func (s *Store) ListPrefixChecks() (map[string][]model.PrefixCheck, error) {
	rows, err := s.db.Query(`SELECT line, prefix, colo, latency, samples, ok_count, checked_at
		FROM candidate_prefix_checks ORDER BY line, prefix`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]model.PrefixCheck{}
	for rows.Next() {
		var line string
		var c model.PrefixCheck
		var ts int64
		if err := rows.Scan(&line, &c.Prefix, &c.Colo, &c.Latency, &c.Samples, &c.OKCount, &ts); err != nil {
			return nil, err
		}
		c.CheckedAt = time.Unix(ts, 0)
		out[line] = append(out[line], c)
	}
	return out, rows.Err()
}
