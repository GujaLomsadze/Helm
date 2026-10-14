package schedule

import (
	"testing"
	"time"
)

func instant(t *testing.T, s string) int64 {
	t.Helper()
	x, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return x.UnixMilli()
}

func TestNextOccurrencesAndMissedIntervals(t *testing.T) {
	anchor := instant(t, "2026-10-14T09:00:00Z")
	for _, tc := range []struct {
		repeat      string
		after, want int64
	}{
		{"once", anchor - 1, anchor}, {"once", anchor, 0},
		{"interval", anchor, anchor + 3600000}, {"interval", anchor + 100*3600000 + 1, anchor + 101*3600000},
		{"daily", anchor, anchor + 24*3600000}, {"weekly", anchor, anchor + 7*24*3600000},
	} {
		c := Config{RunAt: anchor, Repeat: tc.repeat, EveryMinutes: 60, Timezone: "UTC"}
		if got := Next(c, tc.after); got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.repeat, got, tc.want)
		}
	}
}

func TestDailyAndWeeklyDST(t *testing.T) {
	for _, tc := range []struct{ anchor, after, repeat, want string }{
		{"2026-03-28T09:00:00+01:00", "2026-03-28T09:00:00+01:00", "daily", "2026-03-29T09:00:00+02:00"},
		{"2026-03-28T02:30:00+01:00", "2026-03-28T02:30:00+01:00", "daily", "2026-03-30T02:30:00+02:00"},
		{"2026-10-24T02:30:00+02:00", "2026-10-25T02:45:00+02:00", "daily", "2026-10-26T02:30:00+01:00"},
		{"2026-10-18T02:30:00+02:00", "2026-10-25T02:45:00+02:00", "weekly", "2026-11-01T02:30:00+01:00"},
		{"2026-03-22T02:30:00+01:00", "2026-03-22T02:30:00+01:00", "weekly", "2026-04-05T02:30:00+02:00"},
	} {
		c := Config{RunAt: instant(t, tc.anchor), Repeat: tc.repeat, Timezone: "Europe/Berlin"}
		if got := Next(c, instant(t, tc.after)); got != instant(t, tc.want) {
			t.Fatalf("%+v: got %s", tc, time.UnixMilli(got))
		}
	}
}

func TestValidateSchedule(t *testing.T) {
	base := Config{Name: "Task", Provider: "codex", Dir: t.TempDir(), Prompt: "Review this", Repeat: "once", RunAt: time.Now().Add(time.Hour).UnixMilli(), Timezone: "UTC"}
	if err := base.Validate(); err != nil || base.TimeoutMinutes != 60 {
		t.Fatalf("%+v %v", base, err)
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Prompt = "" }, func(c *Config) { c.Dir = "relative" }, func(c *Config) { c.Provider = "shell" },
		func(c *Config) { c.Timezone = "not/a-zone" }, func(c *Config) { c.Repeat = "interval"; c.EveryMinutes = 0 },
		func(c *Config) { c.TimeoutMinutes = -1 }, func(c *Config) { c.Prompt = "hi\x00there" },
	} {
		c := base
		mutate(&c)
		if c.Validate() == nil {
			t.Fatalf("invalid config accepted: %+v", c)
		}
	}
}
