//go:build !windows

package term

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GujaLomsadze/helm/internal/agent"
	_ "github.com/GujaLomsadze/helm/internal/agent/all"
	"github.com/GujaLomsadze/helm/internal/agent/claude"
)

// args is the argv the default agent's adapter builds for a spec.
func args(sp Spec) []string {
	l, _ := claude.Adapter{}.Launch(agent.LaunchSpec{Prompt: sp.Prompt, Resume: sp.Resume, Fork: sp.Fork})
	return l.Args
}

func newTestManager(cmd string) *Manager {
	m := NewManager()
	m.Command = cmd
	return m
}

// wait polls cond for up to 3 s.
func wait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestStartAttachWriteStop(t *testing.T) {
	m := newTestManager("cat")
	var changes atomic.Int32
	m.OnChange = func() { changes.Add(1) }
	tm, err := m.Start(Spec{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	snap, ch, detach := tm.Attach()
	defer detach()
	if len(snap) != 0 {
		t.Fatalf("fresh terminal has scrollback %q", snap)
	}
	if err := tm.Write([]byte("hello-pty\n")); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	wait(t, "echo from cat", func() bool {
		for {
			select {
			case b := <-ch:
				got.Write(b)
			default:
				return strings.Count(got.String(), "hello-pty") >= 2 // tty echo + cat's copy
			}
		}
	})
	// a second viewer gets the scrollback so far
	snap2, _, detach2 := tm.Attach()
	detach2()
	if !bytes.Contains(snap2, []byte("hello-pty")) {
		t.Fatalf("scrollback = %q", snap2)
	}
	if l := m.List(); len(l) != 1 || l[0].ID != tm.Info().ID || l[0].Exited || m.Running() != 1 {
		t.Fatalf("list = %+v running=%d", l, m.Running())
	}
	if err := m.Stop(tm.Info().ID); err != nil {
		t.Fatal(err)
	}
	if len(m.List()) != 0 || m.Running() != 0 {
		t.Fatalf("after stop list = %+v", m.List())
	}
	if n := changes.Load(); n < 2 {
		t.Fatalf("OnChange called %d times", n)
	}
}

func TestExitIsReported(t *testing.T) {
	m := newTestManager("true")
	tm, err := m.Start(Spec{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, "exit", func() bool { l := m.List(); return len(l) == 1 && l[0].Exited })
	if m.Running() != 0 {
		t.Fatalf("running = %d", m.Running())
	}
	_, ch, _ := tm.Attach()
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("attach to an exited terminal should give a closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed")
	}
}

func TestStartRejectsBadDir(t *testing.T) {
	m := newTestManager("cat")
	for _, d := range []string{"", "relative/dir", "/definitely/not/here"} {
		if _, err := m.Start(Spec{Dir: d}); err == nil {
			t.Errorf("dir %q accepted", d)
		}
	}
}

func TestArgsPassPromptAsOneArgument(t *testing.T) {
	if got := args(Spec{Prompt: "  fix it; rm -rf / \"$HOME\"  "}); !slices.Equal(got, []string{`fix it; rm -rf / "$HOME"`}) {
		t.Fatalf("args = %q", got)
	}
	if got := args(Spec{Prompt: "   "}); len(got) != 0 {
		t.Fatalf("blank prompt args = %q", got)
	}
}

func TestArgsResumeAndFork(t *testing.T) {
	id := "7f3a9c2e-51d0-4b8e-9e0a-2c6f1d4b88a1"
	if got := args(Spec{Resume: id}); !slices.Equal(got, []string{"--resume", id}) {
		t.Fatalf("resume args = %q", got)
	}
	if got := args(Spec{Resume: id, Fork: true, Prompt: "go on"}); !slices.Equal(got, []string{"--resume", id, "--fork-session", "go on"}) {
		t.Fatalf("fork args = %q", got)
	}
}

func TestStartValidatesResume(t *testing.T) {
	m := newTestManager("cat")
	t.Cleanup(m.Close)
	dir := t.TempDir()
	for _, sp := range []Spec{{Dir: dir, Resume: "--dangerously-skip-permissions"}, {Dir: dir, Resume: "a b"}, {Dir: dir, Fork: true}} {
		if _, err := m.Start(sp); err == nil {
			t.Errorf("spec %+v accepted", sp)
		}
	}
	tm, err := m.Start(Spec{Dir: dir, Resume: "abc-123"})
	if err != nil {
		t.Fatal(err)
	}
	if tm.Info().SessionID != "abc-123" {
		t.Fatalf("resumed terminal not linked: %+v", tm.Info())
	}
	if !m.Link(tm.Info().ID, "def-456") || m.Link(tm.Info().ID, "def-456") || tm.Info().SessionID != "def-456" {
		t.Fatal("Link did not update once")
	}
}

func TestStartLimitAndMissingCommand(t *testing.T) {
	m := newTestManager("cat")
	t.Cleanup(m.Close)
	dir := t.TempDir()
	for i := 0; i < MaxRunning; i++ {
		if _, err := m.Start(Spec{Dir: dir}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Start(Spec{Dir: dir}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("over the limit: %v", err)
	}
	m2 := newTestManager("hlm-no-such-command-xyz")
	if _, err := m2.Start(Spec{Dir: dir}); err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("missing command: %v", err)
	}
}

// The daemon is usually spawned from inside a Claude session; the child must not
// inherit that session's identity or it runs as a sub-agent without a transcript.
func TestCleanEnvDropsParentClaudeSession(t *testing.T) {
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "parent")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_PID", "42")
	t.Setenv("HLM_KEEP_ME", "yes")
	t.Setenv("CODEX_THREAD_ID", "thread")
	t.Setenv("HLM_SESSION", "outer")
	t.Setenv("HLM_TERMINAL_ID", "outer-term")
	env := CleanEnv()
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDE") || strings.HasPrefix(kv, "CODEX_THREAD") || strings.HasPrefix(kv, "HLM_SESSION") || strings.HasPrefix(kv, "HLM_TERMINAL") {
			t.Errorf("leaked %s", kv)
		}
	}
	if !slices.Contains(env, "HLM_KEEP_ME=yes") || !slices.Contains(env, "PATH="+os.Getenv("PATH")) {
		t.Error("dropped unrelated variables")
	}
}

func TestScrollbackIsCapped(t *testing.T) {
	var r ring
	r.max = 10
	r.write([]byte("0123456789"))
	r.write([]byte("abc"))
	if got := string(r.bytes()); got != "3456789abc" {
		t.Fatalf("ring = %q", got)
	}
}

func TestTerminalIDAndLinkFirst(t *testing.T) {
	// A stand-in that ignores codex's arguments and keeps running (sh would exit at once
	// on --no-daemon, and an exited terminal never links).
	fake := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestManager(fake)
	t.Cleanup(m.Close)
	tm, err := m.Start(Spec{Dir: t.TempDir(), Agent: "codex", Prompt: "-c"})
	if err != nil {
		t.Fatal(err)
	}
	if tm.Info().Agent != "codex" {
		t.Fatal(tm.Info())
	}
	if !m.LinkFirst(tm.Info().ID, "codex:a") || m.LinkFirst(tm.Info().ID, "codex:b") || tm.Info().SessionID != "codex:a" {
		t.Fatal("first session must stick")
	}
	if _, err := m.Start(Spec{Dir: t.TempDir(), Agent: "nope"}); err == nil {
		t.Fatal("unknown agent accepted")
	}
	r, _ := m.Start(Spec{Dir: t.TempDir(), Agent: "codex", Resume: "abc"})
	if r.Info().SessionID != "codex:abc" {
		t.Fatal(r.Info())
	}
}
