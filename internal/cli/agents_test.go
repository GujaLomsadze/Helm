package cli

import (
	"strings"
	"testing"
)

func TestHookForCodex(t *testing.T) {
	got := capture(t)
	t.Setenv("TRK_TERMINAL_ID", "term-1")
	code, out := runIn(`{"session_id":"native","hook_event_name":"PreToolUse"}`, "hook", "--agent", "codex")
	e := (*got)[0]
	if code != 0 || out != "" || len(*got) != 1 || e.Agent != "codex" || e.ClaudePID != 0 || e.TerminalID != "term-1" {
		t.Fatalf("%d %q %+v", code, out, *got)
	}
	// Codex waits for JSON from a Stop handler.
	if code, out = runIn(`{"session_id":"native","hook_event_name":"Stop"}`, "hook", "--agent=codex"); code != 0 || strings.TrimSpace(out) != "{}" {
		t.Fatalf("Stop: %d %q", code, out)
	}
	// Claude hooks keep their pid and gain the terminal id; agent stays implicit.
	runIn(`{"session_id":"c","hook_event_name":"Stop"}`, "hook")
	if e := (*got)[len(*got)-1]; e.Agent != "" || e.ClaudePID != 4242 || e.TerminalID != "term-1" {
		t.Fatalf("%+v", e)
	}
	n := len(*got)
	if code, _ := runIn(`{"session_id":"x"}`, "hook", "--agent", "nope"); code != 0 || len(*got) != n {
		t.Fatal("unknown agent forwarded")
	}
}

func TestStepFindsCodexThread(t *testing.T) {
	got := capture(t)
	t.Setenv("CODEX_THREAD_ID", "thread-9")
	t.Setenv("TRK_SESSION", "")
	findClaude = func() int { return 0 }
	run("step", "testing")
	if e := (*got)[0]; e.SessionID != "codex:thread-9" || e.SessionVia != "" {
		t.Fatalf("%+v", e)
	}
	// TRK_SESSION still wins, and a Claude ancestor wins over an inherited Codex variable.
	t.Setenv("TRK_SESSION", "mine")
	run("step", "x")
	if e := (*got)[1]; e.SessionID != "mine" {
		t.Fatalf("%+v", e)
	}
	t.Setenv("TRK_SESSION", "")
	findClaude = func() int { return 77 }
	run("step", "x")
	if e := (*got)[2]; e.SessionID != "" || e.ClaudePID != 77 {
		t.Fatalf("%+v", e)
	}
}
