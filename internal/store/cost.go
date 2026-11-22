package store

import "database/sql"

// Cost ledger: how much each session spent in each hour, by the agent's own estimate.
// Raw events get trimmed when the database grows; this table doesn't, so budgets and the
// cost charts keep their history.

const hourMs = 3600 * 1000

// costBackfill fills the ledger from the status-line events still stored, the first time
// the ledger exists. Counters are cumulative per agent process: a drop starts a new one.
const costBackfill = `
INSERT INTO cost_hours(session_id, hour, usd)
SELECT session_id, ts / 3600000 * 3600000, sum(d) FROM (
  SELECT session_id, ts, CASE WHEN p IS NULL OR c < p THEN c ELSE c - p END AS d FROM (
    SELECT session_id, ts, json_extract(payload,'$.cost.total_cost_usd') AS c,
      lag(json_extract(payload,'$.cost.total_cost_usd')) OVER (PARTITION BY session_id ORDER BY ts, id) AS p
    FROM events WHERE kind = 'status' AND session_id IS NOT NULL AND session_id != ''
      AND json_extract(payload,'$.cost.total_cost_usd') IS NOT NULL))
WHERE d > 0 GROUP BY session_id, ts / 3600000 * 3600000;`

// AddCost adds usd to the session's spend in the hour containing ts.
func (s *Store) AddCost(session string, ts int64, usd float64) error {
	if usd <= 0 || session == "" {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO cost_hours(session_id, hour, usd) VALUES(?,?,?)
ON CONFLICT(session_id, hour) DO UPDATE SET usd = usd + excluded.usd`, session, ts/hourMs*hourMs, usd)
	return err
}

// CostHour is one session's spend in one hour.
type CostHour struct {
	Session string
	Hour    int64 // unix ms at the start of the hour
	USD     float64
}

// CostHours returns the ledger rows from the hour containing since onwards.
func (s *Store) CostHours(since int64) ([]CostHour, error) {
	rows, err := s.db.Query(`SELECT session_id, hour, usd FROM cost_hours WHERE hour >= ? ORDER BY hour`, since/hourMs*hourMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CostHour
	for rows.Next() {
		var c CostHour
		if err := rows.Scan(&c.Session, &c.Hour, &c.USD); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LastCost is the session's latest cumulative cost reading before ts, the baseline the
// next one grew from.
func (s *Store) LastCost(session string, before int64) (float64, bool, error) {
	var c float64
	err := s.db.QueryRow(`SELECT json_extract(payload,'$.cost.total_cost_usd') FROM events
WHERE session_id = ? AND ts < ? AND kind = 'status' AND json_extract(payload,'$.cost.total_cost_usd') IS NOT NULL
ORDER BY ts DESC, id DESC LIMIT 1`, session, before).Scan(&c)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return c, err == nil, err
}

// SetSessionProfile records the launch profile a session runs with ("" removes it).
func (s *Store) SetSessionProfile(session, profile string, ts int64) error {
	if profile == "" {
		_, err := s.db.Exec(`DELETE FROM session_profiles WHERE session_id = ?`, session)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO session_profiles(session_id, profile_id, ts) VALUES(?,?,?)
ON CONFLICT(session_id) DO UPDATE SET profile_id = excluded.profile_id, ts = excluded.ts`, session, profile, ts)
	return err
}

// SessionProfiles maps every session that ran with a profile to it.
func (s *Store) SessionProfiles() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT session_id, profile_id FROM session_profiles`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var sid, pid string
		if err := rows.Scan(&sid, &pid); err != nil {
			return nil, err
		}
		out[sid] = pid
	}
	return out, rows.Err()
}
