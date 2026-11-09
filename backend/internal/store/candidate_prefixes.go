package store

import (
	"database/sql"
	"time"
)

// Candidate prefixes: a static, operator-supplied list of /24 blocks per line.
//
// 这张表只由人手写（设置页导入），扫描流程只读。没有任何「扫完自动收录」
// 的路径 —— 那会让系统用几轮前的结论决定下一轮的候选，把实时测量变成自我
// 验证。这里存的只是「往哪看」，测出来什么、选谁下发，仍然由当轮实测决定。

// CandidatePrefixes returns the prefixes configured for one line.
//
// 不按延迟排序：表里根本没有延迟字段。清单是配置，不是成绩单。
func (s *Store) CandidatePrefixes(line string) []string {
	rows, err := s.db.Query(`SELECT prefix FROM candidate_prefixes WHERE line = ? ORDER BY prefix`, line)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// ListCandidatePrefixes returns every line's configuration, grouped by line.
func (s *Store) ListCandidatePrefixes() (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT line, prefix FROM candidate_prefixes ORDER BY line, prefix`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var line, prefix string
		if err := rows.Scan(&line, &prefix); err != nil {
			return nil, err
		}
		out[line] = append(out[line], prefix)
	}
	return out, rows.Err()
}

// ReplaceCandidatePrefixes overwrites one line's list atomically.
//
// 整体替换而不是增量合并：导入一份新清单就该完全取代旧的，不会出现
// 「旧前缀永远删不掉」的累积。
func (s *Store) ReplaceCandidatePrefixes(line string, prefixes []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM candidate_prefixes WHERE line = ?`, line); err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, p := range prefixes {
		if _, err := tx.Exec(
			`INSERT INTO candidate_prefixes (line, prefix, note, created_at) VALUES (?, ?, '', ?)
			 ON CONFLICT(line, prefix) DO NOTHING`, line, p, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteCandidatePrefix removes one entry, returning sql.ErrNoRows when absent
// so the API can answer 404 instead of pretending it worked.
func (s *Store) DeleteCandidatePrefix(line, prefix string) error {
	res, err := s.db.Exec(`DELETE FROM candidate_prefixes WHERE line = ? AND prefix = ?`, line, prefix)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
