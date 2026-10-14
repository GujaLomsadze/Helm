package store

import (
	"errors"
	"testing"

	"github.com/GujaLomsadze/trk/internal/schedule"
)

func TestSchedulesClaimRecoveryAndPersistence(t *testing.T) {
	s, path := open(t)
	j := schedule.Job{ID: "job", Enabled: true, NextRunAt: 1000, CreatedAt: 500, Config: schedule.Config{Name: "Task", Provider: "codex", Prompt: "literal", Repeat: "interval", EveryMinutes: 1}}
	if err := s.SaveSchedule(j, true); err != nil {
		t.Fatal(err)
	}
	r := schedule.Run{ID: "run", JobID: j.ID, ScheduledAt: 1000, StartedAt: 1000}
	if err := s.ClaimSchedule(j, r, true, 61000); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimSchedule(j, schedule.Run{ID: "double"}, true, 61000); !errors.Is(err, ErrScheduleConflict) {
		t.Fatalf("double launch: %v", err)
	}
	if err := s.DeleteSchedule(j.ID); !errors.Is(err, ErrScheduleConflict) {
		t.Fatalf("deleted running task: %v", err)
	}
	if err := s.SaveSchedule(j, false); !errors.Is(err, ErrScheduleConflict) {
		t.Fatalf("edited running task: %v", err)
	}
	if err := s.SetScheduleEnabled(j.ID, false, 61000); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.RecoverScheduleRuns(2000); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.Schedules()
	if err != nil || len(jobs) != 1 || jobs[0].NextRunAt != 61000 || jobs[0].Enabled {
		t.Fatalf("jobs %+v, %v", jobs, err)
	}
	runs, err := s.ScheduleRuns(j.ID, true)
	if err != nil || len(runs) != 1 || runs[0].Status != "interrupted" {
		t.Fatalf("runs %+v %v", runs, err)
	}
	if err := s.DeleteSchedule(j.ID); err != nil {
		t.Fatal(err)
	}
	runs, err = s.ScheduleRuns(j.ID, true)
	if err != nil || len(runs) != 0 {
		t.Fatal("history was not removed", err)
	}
}

func TestScheduledRunHistoryIsBounded(t *testing.T) {
	s, _ := open(t)
	j := schedule.Job{ID: "job", Enabled: true, NextRunAt: 1000, Config: schedule.Config{Name: "Task"}}
	if err := s.SaveSchedule(j, true); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 35; i++ {
		r := schedule.Run{ID: schedule.ID(), JobID: j.ID, StartedAt: i, Status: "succeeded", Output: "result"}
		if err := s.ClaimSchedule(j, r, false, 0); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveScheduleRun(r); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := s.ScheduleRuns(j.ID, true)
	if err != nil || len(runs) != 30 || runs[0].StartedAt != 35 || runs[29].StartedAt != 6 || runs[0].Output != "result" {
		t.Fatalf("runs %+v %v", runs, err)
	}
}
