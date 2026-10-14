package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexHookAndReporting(t *testing.T) {
	got := capture(t)
	t.Setenv("TRK_SESSION", "wrong-inherited-id")
	code, out := runIn(`{"session_id":"native","hook_event_name":"PreToolUse"}`, "codex-hook")
	if code != 0 || out != "" || len(*got) != 1 || (*got)[0].Provider != "codex" || (*got)[0].ClaudePID != 0 || (*got)[0].SessionID != "" {
		t.Fatalf("%d %q %+v", code, out, *got)
	}
	code, out = runIn(`{"session_id":"native","hook_event_name":"Stop"}`, "codex-hook")
	if code != 0 || strings.TrimSpace(out) != "{}" {
		t.Fatalf("Stop: %d %q", code, out)
	}
	n := len(*got)
	for _, raw := range []string{"", "null", "[]", "invalid", `{"session_id":null}`} {
		runIn(raw, "codex-hook")
	}
	if len(*got) != n {
		t.Fatal("forwarded malformed hooks")
	}
	t.Setenv("CODEX_THREAD_ID", "native")
	run("codex", "step", "testing")
	e := (*got)[len(*got)-1]
	if e.SessionID != "wrong-inherited-id" || e.Provider != "codex" || e.ClaudePID != 0 {
		t.Fatal(e)
	}
	t.Setenv("TRK_SESSION", "")
	run("codex", "progress", "2/3")
	e = (*got)[len(*got)-1]
	if e.SessionID != "native" || e.Kind != "progress" {
		t.Fatal(e)
	}
}

func TestCodexInitDryRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	code, out, stderr := run("init", "--agent", "codex", "--dry-run")
	if code != 0 || stderr != "" || !strings.Contains(out, "codex-hook") || !strings.Contains(out, "/hooks") {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "hooks.json")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote configuration")
	}
}
