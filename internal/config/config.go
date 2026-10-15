// Package config resolves Helm's environment-driven settings. Everything is optional.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const DefaultPort = 7777

// Env reads HLM_<key>, falling back to TRK_<key>: Helm was called TRK, and agents
// started by an older daemon, scripts and shell profiles still set the old names.
func Env(key string) string {
	if v := os.Getenv("HLM_" + key); v != "" {
		return v
	}
	return os.Getenv("TRK_" + key)
}

func Port() int {
	if p, err := strconv.Atoi(Env("PORT")); err == nil && p > 0 && p < 65536 {
		return p
	}
	return DefaultPort
}

// BaseURL is where clients send events. HLM_URL wins (e.g. WSL ↔ Windows).
func BaseURL() string {
	if u := strings.TrimRight(Env("URL"), "/"); u != "" {
		return u
	}
	return fmt.Sprintf("http://127.0.0.1:%d", Port())
}

// IsLocalDefault reports whether clients talk to a daemon this machine may spawn.
func IsLocalDefault() bool { return Env("URL") == "" }

func DataDir() (string, error) {
	if d := Env("DATA_DIR"); d != "" {
		return d, nil
	}
	return defaultDataDir("hlm")
}

// defaultDataDir is the per-user data folder for an app name.
func defaultDataDir(name string) (string, error) {
	home, err := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", name), nil
	case "windows":
		if a := os.Getenv("APPDATA"); a != "" {
			return filepath.Join(a, name), nil
		}
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, name), nil
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", name), nil
}

// legacyDataDir is where TRK kept its data, when no data dir is set explicitly.
func legacyDataDir() string {
	if Env("DATA_DIR") != "" {
		return ""
	}
	d, err := defaultDataDir("trk")
	if err != nil {
		return ""
	}
	return d
}

// MigrateDataDir moves TRK's data folder to Helm's on first start, renaming the files
// inside (trk.db → hlm.db, …). Only the daemon calls it, after binding the port, so no
// older daemon still has the database open. It reports whether it moved anything.
func MigrateDataDir() (bool, error) {
	old := legacyDataDir()
	dir, err := DataDir()
	if old == "" || err != nil {
		return false, err
	}
	if _, err := os.Stat(dir); err == nil {
		return false, nil // already migrated, or a fresh Helm install
	}
	if st, err := os.Stat(old); err != nil || !st.IsDir() {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(old, dir); err != nil {
		return false, err
	}
	for _, ext := range []string{".db", ".db-wal", ".db-shm", ".log"} {
		if _, err := os.Stat(filepath.Join(dir, "trk"+ext)); err == nil {
			if err := os.Rename(filepath.Join(dir, "trk"+ext), filepath.Join(dir, "hlm"+ext)); err != nil {
				return true, err
			}
		}
	}
	_ = os.Remove(filepath.Join(dir, "trk.pid"))
	return true, nil
}

// The pause flag is a file in the data dir. While it exists, every client call
// is a silent no-op and nothing auto-spawns the daemon (`hlm stop` sets it,
// `hlm open` / `hlm serve` clear it).
func pausedFile() string {
	dir, err := DataDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "paused")
}

func Paused() bool {
	f := pausedFile()
	if f == "" {
		return false
	}
	if _, err := os.Stat(f); err == nil {
		return true
	}
	// Paused as TRK and not migrated yet: still paused.
	if old := legacyDataDir(); old != "" {
		if _, err := os.Stat(filepath.Dir(f)); os.IsNotExist(err) {
			_, err := os.Stat(filepath.Join(old, "paused"))
			return err == nil
		}
	}
	return false
}

func SetPaused(on bool) error {
	f := pausedFile()
	if f == "" {
		return fmt.Errorf("no data dir")
	}
	if !on {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f, []byte("paused by hlm stop\n"), 0o644)
}

// MaxDBBytes caps the event database (HLM_MAX_DB_MB, default 5 GB). Past it,
// the oldest raw events are trimmed; session summaries stay.
func MaxDBBytes() int64 {
	if mb, err := strconv.ParseInt(Env("MAX_DB_MB"), 10, 64); err == nil && mb > 0 {
		return mb << 20
	}
	return 5 << 30
}
