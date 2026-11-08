package store

import "edgepilot/internal/model"

// SetDomainProbes replaces the probe bindings for one domain. The denormalised
// domains.probe_id column is kept in sync with the first id so older readers
// (and the API's legacy single-probe field) stay correct.
func (s *Store) SetDomainProbes(domainID int64, ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM domain_probes WHERE domain_id=?`, domainID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO domain_probes(domain_id, probe_id) VALUES(?,?)`,
			domainID, id); err != nil {
			return err
		}
	}
	primary := int64(0)
	if len(ids) > 0 {
		primary = ids[0]
	}
	if _, err := tx.Exec(`UPDATE domains SET probe_id=? WHERE id=?`, primary, domainID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DomainProbeIDs(domainID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT probe_id FROM domain_probes WHERE domain_id=? ORDER BY probe_id`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DomainProbeMap loads every binding in one query so listing domains does not
// turn into N+1 round trips.
func (s *Store) DomainProbeMap() (map[int64][]int64, error) {
	rows, err := s.db.Query(`SELECT domain_id, probe_id FROM domain_probes ORDER BY domain_id, probe_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var did, pid int64
		if err := rows.Scan(&did, &pid); err != nil {
			return nil, err
		}
		out[did] = append(out[did], pid)
	}
	return out, rows.Err()
}

// attachProbes fills Domain.ProbeIDs for a slice of domains in one pass.
func (s *Store) attachProbes(list []model.Domain) error {
	if len(list) == 0 {
		return nil
	}
	all, err := s.DomainProbeMap()
	if err != nil {
		return err
	}
	for i := range list {
		list[i].ProbeIDs = all[list[i].ID]
	}
	return nil
}
