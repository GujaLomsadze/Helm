package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/GujaLomsadze/trk/internal/schedule"
)

var ErrScheduleConflict = errors.New("schedule is running or has changed; refresh and try again")
var ErrScheduleNotFound = errors.New("schedule not found")

func (s *Store) SetScheduleEnabled(id string, enabled bool, next int64) error {
	_, err := s.db.Exec(`UPDATE schedules SET enabled=?,next_run_at=? WHERE id=?`, enabled, next, id)
	return err
}

func (s *Store) Schedules() ([]schedule.Job, error) {
	rows, err := s.db.Query(`SELECT id, config, enabled, next_run_at, created_at FROM schedules ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []schedule.Job{}
	for rows.Next() {
		var j schedule.Job
		var raw string
		if err := rows.Scan(&j.ID, &raw, &j.Enabled, &j.NextRunAt, &j.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &j.Config); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) SaveSchedule(j schedule.Job, create bool) error {
	b, err := json.Marshal(j.Config)
	if err != nil {
		return err
	}
	if create {
		_, err = s.db.Exec(`INSERT INTO schedules(id,config,enabled,next_run_at,created_at) VALUES(?,?,?,?,?)`, j.ID, string(b), j.Enabled, j.NextRunAt, j.CreatedAt)
		return err
	}
	res, err := s.db.Exec(`UPDATE schedules SET config=?,enabled=?,next_run_at=? WHERE id=? AND NOT EXISTS(SELECT 1 FROM schedule_runs WHERE job_id=? AND status='running')`, string(b), j.Enabled, j.NextRunAt, j.ID, j.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrScheduleConflict
	}
	return nil
}

func (s *Store) DeleteSchedule(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var running int
	if err := tx.QueryRow(`SELECT count(*) FROM schedule_runs WHERE job_id=? AND status='running'`, id).Scan(&running); err != nil {
		return err
	}
	if running != 0 {
		return ErrScheduleConflict
	}
	res, err := tx.Exec(`DELETE FROM schedules WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrScheduleNotFound
	}
	if _, err := tx.Exec(`DELETE FROM schedule_runs WHERE job_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimSchedule persists intent before spawning a process. A crash can mark a run
// interrupted, but can never replay that occurrence and launch it twice.
func (s *Store) ClaimSchedule(j schedule.Job, r schedule.Run, due bool, next int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled bool
	var expected int64
	if err := tx.QueryRow(`SELECT enabled,next_run_at FROM schedules WHERE id=?`, j.ID).Scan(&enabled, &expected); err != nil {
		return err
	}
	if due && (!enabled || expected != j.NextRunAt || expected == 0) {
		return ErrScheduleConflict
	}
	var running int
	if err := tx.QueryRow(`SELECT count(*) FROM schedule_runs WHERE job_id=? AND status='running'`, j.ID).Scan(&running); err != nil {
		return err
	}
	if running != 0 {
		return ErrScheduleConflict
	}
	if _, err := tx.Exec(`INSERT INTO schedule_runs(id,job_id,status,scheduled_at,started_at) VALUES(?,?,'running',?,?)`, r.ID, j.ID, r.ScheduledAt, r.StartedAt); err != nil {
		return err
	}
	if due {
		if _, err := tx.Exec(`UPDATE schedules SET next_run_at=? WHERE id=?`, next, j.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SaveScheduleRun(r schedule.Run) error {
	_, err := s.db.Exec(`UPDATE schedule_runs SET status=?,finished_at=?,terminal_id=?,session_id=?,exit_code=?,error=?,output=? WHERE id=?`, r.Status, r.FinishedAt, r.TerminalID, r.SessionID, r.ExitCode, r.Error, r.Output, r.ID)
	if err != nil {
		return err
	}
	if r.Status != "running" {
		_, err = s.db.Exec(`DELETE FROM schedule_runs WHERE job_id=? AND status!='running' AND id NOT IN (SELECT id FROM schedule_runs WHERE job_id=? ORDER BY started_at DESC,id DESC LIMIT 30)`, r.JobID, r.JobID)
	}
	return err
}

func (s *Store) RecoverScheduleRuns(now int64) error {
	_, err := s.db.Exec(`UPDATE schedule_runs SET status='interrupted',finished_at=?,error='TRK stopped before this run finished; the task was not retried automatically' WHERE status='running'`, now)
	return err
}

func (s *Store) ScheduleRuns(jobID string, output bool) ([]schedule.Run, error) {
	cols := `id,job_id,status,scheduled_at,started_at,finished_at,terminal_id,session_id,exit_code,error`
	if output {
		cols += `,output`
	}
	rows, err := s.db.Query(`SELECT `+cols+` FROM schedule_runs WHERE job_id=? ORDER BY started_at DESC,id DESC LIMIT 30`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []schedule.Run{}
	for rows.Next() {
		var r schedule.Run
		var code sql.NullInt64
		args := []any{&r.ID, &r.JobID, &r.Status, &r.ScheduledAt, &r.StartedAt, &r.FinishedAt, &r.TerminalID, &r.SessionID, &code, &r.Error}
		if output {
			args = append(args, &r.Output)
		}
		if err := rows.Scan(args...); err != nil {
			return nil, err
		}
		if code.Valid {
			n := int(code.Int64)
			r.ExitCode = &n
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
