// Package schedule defines persisted agent tasks and their calendar recurrence.
package schedule

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata" // Windows installations need not have an IANA timezone database.
	"unicode/utf8"
)

type Config struct {
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Dir            string `json:"dir"`
	Prompt         string `json:"prompt"`
	Repeat         string `json:"repeat"`
	RunAt          int64  `json:"run_at"`
	EveryMinutes   int    `json:"every_minutes"`
	Timezone       string `json:"timezone"`
	TimeoutMinutes int    `json:"timeout_minutes"`
}

type Job struct {
	Config
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	NextRunAt int64  `json:"next_run_at"`
	CreatedAt int64  `json:"created_at"`
}

type Run struct {
	ID          string `json:"id"`
	JobID       string `json:"job_id"`
	Status      string `json:"status"`
	ScheduledAt int64  `json:"scheduled_at"`
	StartedAt   int64  `json:"started_at"`
	FinishedAt  int64  `json:"finished_at"`
	TerminalID  string `json:"terminal_id"`
	SessionID   string `json:"session_id"`
	ExitCode    *int   `json:"exit_code"`
	Error       string `json:"error"`
	Output      string `json:"output,omitempty"`
}

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (c *Config) Validate() error {
	c.Name, c.Prompt = strings.TrimSpace(c.Name), strings.TrimSpace(c.Prompt)
	if c.Name == "" || utf8.RuneCountInString(c.Name) > 80 || strings.ContainsAny(c.Name, "\r\n\x00") {
		return errors.New("name must contain 1–80 characters on one line")
	}
	if c.Provider != "claude" && c.Provider != "codex" {
		return errors.New("choose Claude or Codex")
	}
	if c.Prompt == "" || len(c.Prompt) > 24<<10 {
		return errors.New("prompt is required (maximum 24 KiB)")
	}
	if strings.ContainsRune(c.Prompt, 0) {
		return errors.New("prompt contains a null character")
	}
	if !filepath.IsAbs(c.Dir) {
		return errors.New("folder must be an absolute path")
	}
	c.Dir = filepath.Clean(c.Dir)
	if st, err := os.Stat(c.Dir); err != nil || !st.IsDir() {
		return errors.New("folder does not exist")
	}
	if c.RunAt <= 0 || c.RunAt > time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		return errors.New("choose a valid start date before 2100")
	}
	if c.Timezone == "" {
		c.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return errors.New("unknown timezone; use an IANA name such as Europe/Berlin")
	}
	switch c.Repeat {
	case "once", "daily", "weekly":
		c.EveryMinutes = 0
	case "interval":
		if c.EveryMinutes < 1 || c.EveryMinutes > 43200 {
			return errors.New("interval must be between 1 and 43200 minutes")
		}
	default:
		return errors.New("choose once, interval, daily, or weekly")
	}
	if c.TimeoutMinutes == 0 {
		c.TimeoutMinutes = 60
	}
	if c.TimeoutMinutes < 1 || c.TimeoutMinutes > 1440 {
		return errors.New("run timeout must be between 1 and 1440 minutes")
	}
	return nil
}

// Next returns the first occurrence strictly after after. Missed occurrences are
// coalesced by the caller. Daily/weekly schedules keep their local wall time;
// nonexistent spring-forward times are skipped and repeated hours run once.
func Next(c Config, after int64) int64 {
	if c.RunAt > after {
		return c.RunAt
	}
	if c.Repeat == "once" {
		return 0
	}
	if c.Repeat == "interval" {
		step := int64(c.EveryMinutes) * 60000
		if step <= 0 {
			return 0
		}
		return c.RunAt + ((after-c.RunAt)/step+1)*step
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return 0
	}
	anchor := time.UnixMilli(c.RunAt).In(loc)
	day := time.UnixMilli(after).In(loc)
	for i := 0; i < 15; i++ {
		date := time.Date(day.Year(), day.Month(), day.Day()+i, 12, 0, 0, 0, loc)
		candidate := time.Date(date.Year(), date.Month(), date.Day(), anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), loc)
		// time.Date may choose either side of a repeated autumn hour. Always
		// use its first occurrence so a tick in between cannot launch it twice.
		start, _ := candidate.ZoneBounds()
		if !start.IsZero() {
			_, before := start.Add(-time.Nanosecond).Zone()
			_, current := candidate.Zone()
			if before > current {
				earlier := candidate.Add(-time.Duration(before-current) * time.Second)
				if earlier.Year() == candidate.Year() && earlier.YearDay() == candidate.YearDay() && earlier.Hour() == candidate.Hour() && earlier.Minute() == candidate.Minute() {
					candidate = earlier
				}
			}
		}
		if candidate.Hour() != anchor.Hour() || candidate.Minute() != anchor.Minute() {
			continue
		}
		if c.Repeat == "weekly" && candidate.Weekday() != anchor.Weekday() {
			continue
		}
		if candidate.UnixMilli() > after && candidate.UnixMilli() >= c.RunAt {
			return candidate.UnixMilli()
		}
	}
	return 0
}
