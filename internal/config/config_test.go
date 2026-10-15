package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBaseURL(t *testing.T) {
	cases := []struct {
		name, port, url, want string
		local                 bool
	}{
		{"default", "", "", "http://127.0.0.1:7777", true},
		{"port", "9000", "", "http://127.0.0.1:9000", true},
		{"bad port", "nope", "", "http://127.0.0.1:7777", true},
		{"out of range", "70000", "", "http://127.0.0.1:7777", true},
		{"url override", "9000", "http://host.docker.internal:7777/", "http://host.docker.internal:7777", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HLM_PORT", c.port)
			t.Setenv("HLM_URL", c.url)
			if got := BaseURL(); got != c.want {
				t.Fatalf("BaseURL() = %q, want %q", got, c.want)
			}
			if got := IsLocalDefault(); got != c.local {
				t.Fatalf("IsLocalDefault() = %v, want %v", got, c.local)
			}
		})
	}
}

func TestDataDirOverride(t *testing.T) {
	t.Setenv("HLM_DATA_DIR", "/tmp/hlm-x")
	got, err := DataDir()
	if err != nil || got != "/tmp/hlm-x" {
		t.Fatalf("DataDir() = %q, %v", got, err)
	}
}

func TestPausedFlag(t *testing.T) {
	t.Setenv("HLM_DATA_DIR", t.TempDir()+"/nested")
	if Paused() {
		t.Fatal("paused before anything set it")
	}
	if err := SetPaused(true); err != nil || !Paused() {
		t.Fatalf("SetPaused(true): err=%v paused=%v", err, Paused())
	}
	if err := SetPaused(false); err != nil || Paused() {
		t.Fatalf("SetPaused(false): err=%v paused=%v", err, Paused())
	}
	if err := SetPaused(false); err != nil {
		t.Fatalf("clearing twice: %v", err)
	}
}

func TestMaxDBBytes(t *testing.T) {
	for in, want := range map[string]int64{"": 5 << 30, "100": 100 << 20, "0": 5 << 30, "-3": 5 << 30, "junk": 5 << 30} {
		t.Setenv("HLM_MAX_DB_MB", in)
		if got := MaxDBBytes(); got != want {
			t.Errorf("HLM_MAX_DB_MB=%q → %d, want %d", in, got, want)
		}
	}
}

func TestEnvFallsBackToTRK(t *testing.T) {
	t.Setenv("HLM_PORT", "")
	t.Setenv("TRK_PORT", "18123")
	if Port() != 18123 {
		t.Fatal(Port())
	}
	t.Setenv("HLM_PORT", "18124")
	if Port() != 18124 {
		t.Fatal("HLM_ wins")
	}
}

func TestMigrateDataDir(t *testing.T) {
	x := t.TempDir()
	t.Setenv("XDG_DATA_HOME", x)
	t.Setenv("HOME", x)
	t.Setenv("HLM_DATA_DIR", "")
	t.Setenv("TRK_DATA_DIR", "")
	if runtime.GOOS != "linux" {
		t.Skip("default data dir differs")
	}
	old := filepath.Join(x, "trk")
	os.MkdirAll(old, 0o755)
	for _, f := range []string{"trk.db", "trk.db-wal", "trk.log", "trk.pid", "paused", "update-check.json"} {
		os.WriteFile(filepath.Join(old, f), []byte(f), 0o644)
	}
	if !Paused() {
		t.Fatal("paused as TRK, not migrated yet: still paused")
	}
	moved, err := MigrateDataDir()
	if err != nil || !moved {
		t.Fatal(moved, err)
	}
	dir, _ := DataDir()
	for _, f := range []string{"hlm.db", "hlm.db-wal", "hlm.log", "paused", "update-check.json"} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err != nil || len(b) == 0 {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "trk.pid")); !os.IsNotExist(err) {
		t.Error("stale pid kept")
	}
	if moved, _ := MigrateDataDir(); moved {
		t.Fatal("moved twice")
	}
	// An explicit data dir is never moved.
	t.Setenv("TRK_DATA_DIR", t.TempDir())
	if moved, _ := MigrateDataDir(); moved {
		t.Fatal("moved an explicit dir")
	}
}
