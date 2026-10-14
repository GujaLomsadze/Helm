package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/GujaLomsadze/trk/internal/schedule"
	"github.com/GujaLomsadze/trk/internal/store"
	"github.com/GujaLomsadze/trk/internal/term"
)

type scheduledRun struct {
	run        schedule.Run
	terminal   *term.Term
	deadline   int64
	stopReason string
}

// schedulerLoop runs only while this daemon owns the local listener. Recover is
// called before serving requests, so abandoned runs never block their schedule.
func (s *Server) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scheduleTick()
		}
	}
}

func (s *Server) scheduleTick() {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	now := s.now()
	for id, active := range s.scheduled {
		info := active.terminal.Info()
		if !info.Exited && now >= active.deadline && active.stopReason == "" {
			active.stopReason = "Run exceeded its time limit"
			_ = s.terms.Stop(info.ID)
		}
		if !info.Exited {
			continue
		}
		r := active.run
		r.FinishedAt, r.ExitCode, r.SessionID = now, info.ExitCode, info.SessionID
		r.Status = "succeeded"
		if info.ExitCode == nil || *info.ExitCode != 0 {
			r.Status, r.Error = "failed", "Agent exited unsuccessfully; review its output"
		}
		if active.stopReason != "" {
			r.Status, r.Error = "stopped", active.stopReason
		}
		r.Output = runOutput(active.terminal)
		if err := s.st.SaveScheduleRun(r); err != nil {
			s.log.Printf("finish schedule: %v", err)
			continue
		}
		delete(s.scheduled, id)
		// Persist the output before releasing the in-memory terminal scrollback.
		_ = s.terms.Stop(info.ID)
	}
	if !s.termsOn.Load() || !term.Supported {
		return
	}
	jobs, err := s.st.Schedules()
	if err != nil {
		s.log.Printf("list schedules: %v", err)
		return
	}
	for _, j := range jobs {
		if !j.Enabled || j.NextRunAt == 0 || j.NextRunAt > now || s.scheduled[j.ID] != nil {
			continue
		}
		if s.terms.Running() >= term.MaxRunning {
			return
		} // leave due work pending until capacity is free
		if _, err := s.launchScheduled(j, true); err != nil {
			s.log.Printf("scheduled task %s: %v", j.ID, err)
		}
	}
}

// launchScheduled requires scheduleMu, shared with editing, pause, and Run now.
func (s *Server) launchScheduled(j schedule.Job, due bool) (schedule.Run, error) {
	if s.scheduled[j.ID] != nil {
		return schedule.Run{}, store.ErrScheduleConflict
	}
	if !s.termsOn.Load() {
		return schedule.Run{}, errors.New("enable Terminals in Fleet → Card layout before running tasks")
	}
	if s.terms.Running() >= term.MaxRunning {
		return schedule.Run{}, errors.New("all TRK agent slots are busy")
	}
	now := s.now()
	r := schedule.Run{ID: schedule.ID(), JobID: j.ID, Status: "running", StartedAt: now, ScheduledAt: now}
	if due {
		r.ScheduledAt = j.NextRunAt
	}
	if err := s.st.ClaimSchedule(j, r, due, schedule.Next(j.Config, now)); err != nil {
		return r, err
	}
	t, err := s.terms.Start(term.Spec{Batch: true, Provider: j.Provider, Dir: j.Dir, Prompt: j.Prompt, Title: j.Name})
	if err != nil {
		r.Status, r.Error, r.FinishedAt = "failed", err.Error(), s.now()
		if saveErr := s.st.SaveScheduleRun(r); saveErr != nil {
			s.log.Printf("persist launch failure: %v", saveErr)
		}
		return r, nil // launch failure is a recorded attempt, not a retry of the occurrence
	}
	r.TerminalID = t.Info().ID
	s.scheduled[j.ID] = &scheduledRun{run: r, terminal: t, deadline: now + int64(j.TimeoutMinutes)*60000}
	if err := s.st.SaveScheduleRun(r); err != nil {
		s.log.Printf("persist scheduled terminal: %v", err)
	}
	return r, nil
}

func runOutput(t *term.Term) string {
	output := t.Output()
	const limit = 64 << 10
	if len(output) > limit {
		output = "[Earlier output truncated]\n" + output[len(output)-limit:]
	}
	return strings.ToValidUTF8(output, "�")
}

func (s *Server) closeScheduledRuns() {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	for id, active := range s.scheduled {
		r := active.run
		r.Status, r.Error, r.FinishedAt = "interrupted", "TRK shut down during this run", s.now()
		r.SessionID, r.Output = active.terminal.Info().SessionID, runOutput(active.terminal)
		if err := s.st.SaveScheduleRun(r); err != nil {
			s.log.Printf("interrupt scheduled run: %v", err)
		}
		delete(s.scheduled, id)
	}
}

func (s *Server) scheduleError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, store.ErrScheduleNotFound) {
		code = http.StatusNotFound
	}
	if errors.Is(err, store.ErrScheduleConflict) {
		code = http.StatusConflict
	}
	if code == http.StatusInternalServerError {
		s.log.Printf("schedules: %v", err)
		http.Error(w, "could not save schedules", code)
		return
	}
	http.Error(w, err.Error(), code)
}

func (s *Server) findSchedule(id string) (schedule.Job, error) {
	jobs, err := s.st.Schedules()
	if err != nil {
		return schedule.Job{}, err
	}
	for _, j := range jobs {
		if j.ID == id {
			return j, nil
		}
	}
	return schedule.Job{}, store.ErrScheduleNotFound
}

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	jobs, err := s.st.Schedules()
	if err != nil {
		s.scheduleError(w, err)
		return
	}
	type entry struct {
		schedule.Job
		LastRun *schedule.Run `json:"last_run"`
		Running bool          `json:"running"`
	}
	out := make([]entry, 0, len(jobs))
	for _, j := range jobs {
		runs, err := s.st.ScheduleRuns(j.ID, false)
		if err != nil {
			s.scheduleError(w, err)
			return
		}
		e := entry{Job: j, Running: s.scheduled[j.ID] != nil}
		if len(runs) > 0 {
			e.LastRun = &runs[0]
		}
		out = append(out, e)
	}
	writeJSON(w, map[string]any{"schedules": out, "enabled": s.termsOn.Load(), "supported": term.Supported, "now": s.now()})
}

func decodeSchedule(w http.ResponseWriter, r *http.Request) (schedule.Config, bool) {
	var c schedule.Config
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 40<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		http.Error(w, "invalid schedule JSON", http.StatusBadRequest)
		return c, false
	}
	if err := c.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return c, false
	}
	return c, true
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	c, ok := decodeSchedule(w, r)
	if !ok {
		return
	}
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	if c.RunAt <= s.now() {
		http.Error(w, "start time must be in the future", http.StatusBadRequest)
		return
	}
	jobs, err := s.st.Schedules()
	if err != nil {
		s.scheduleError(w, err)
		return
	}
	if len(jobs) >= 100 {
		http.Error(w, "maximum of 100 schedules reached", http.StatusConflict)
		return
	}
	j := schedule.Job{Config: c, ID: schedule.ID(), Enabled: true, NextRunAt: c.RunAt, CreatedAt: s.now()}
	if err := s.st.SaveSchedule(j, true); err != nil {
		s.scheduleError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, j)
}

func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	c, ok := decodeSchedule(w, r)
	if !ok {
		return
	}
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	j, err := s.findSchedule(r.PathValue("id"))
	if err != nil {
		s.scheduleError(w, err)
		return
	}
	if j.RunAt != c.RunAt && c.RunAt <= s.now() {
		http.Error(w, "new start time must be in the future", http.StatusBadRequest)
		return
	}
	if s.scheduled[j.ID] != nil {
		s.scheduleError(w, store.ErrScheduleConflict)
		return
	}
	j.Config = c
	j.NextRunAt = schedule.Next(c, s.now())
	if err := s.st.SaveSchedule(j, false); err != nil {
		s.scheduleError(w, err)
		return
	}
	writeJSON(w, j)
}

func (s *Server) enableSchedule(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&b); err != nil || b.Enabled == nil {
		http.Error(w, "enabled is required", 400)
		return
	}
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	j, err := s.findSchedule(r.PathValue("id"))
	if err != nil {
		s.scheduleError(w, err)
		return
	}
	// Pausing a schedule never kills its current run. The next occurrence is
	// recalculated on resume so paused occurrences aren't replayed.
	if j.Enabled == *b.Enabled {
		writeJSON(w, j)
		return
	}
	j.Enabled = *b.Enabled
	if j.Enabled {
		j.NextRunAt = schedule.Next(j.Config, s.now())
		if j.NextRunAt == 0 {
			http.Error(w, "choose a future start time for this completed task", 409)
			return
		}
	}
	// Config editing is forbidden during a run, but pausing is always allowed.
	if err := s.st.SetScheduleEnabled(j.ID, j.Enabled, j.NextRunAt); err != nil {
		s.scheduleError(w, err)
		return
	}
	writeJSON(w, j)
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	if err := s.st.DeleteSchedule(r.PathValue("id")); err != nil {
		s.scheduleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runScheduleNow(w http.ResponseWriter, r *http.Request) {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	j, err := s.findSchedule(r.PathValue("id"))
	if err != nil {
		s.scheduleError(w, err)
		return
	}
	if !s.termsOn.Load() || !term.Supported {
		http.Error(w, "enable Terminals in Fleet → Card layout before running tasks", http.StatusForbidden)
		return
	}
	run, err := s.launchScheduled(j, false)
	if err != nil {
		if errors.Is(err, store.ErrScheduleConflict) {
			s.scheduleError(w, err)
		} else {
			http.Error(w, err.Error(), http.StatusConflict)
		}
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, run)
}

func (s *Server) stopScheduleRun(w http.ResponseWriter, r *http.Request) {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	a := s.scheduled[r.PathValue("id")]
	if a == nil {
		http.Error(w, "no active run", http.StatusNotFound)
		return
	}
	a.stopReason = "Stopped from the Schedules page"
	_ = s.terms.Stop(a.terminal.Info().ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listScheduleRuns(w http.ResponseWriter, r *http.Request) {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	id := r.PathValue("id")
	if _, err := s.findSchedule(id); err != nil {
		s.scheduleError(w, err)
		return
	}
	runs, err := s.st.ScheduleRuns(id, true)
	if err != nil {
		s.scheduleError(w, err)
		return
	}
	if a := s.scheduled[id]; a != nil {
		for i := range runs {
			if runs[i].ID == a.run.ID {
				runs[i].Output = runOutput(a.terminal)
				runs[i].SessionID = a.terminal.Info().SessionID
			}
		}
	}
	writeJSON(w, map[string]any{"runs": runs})
}
