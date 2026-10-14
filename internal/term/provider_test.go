package term

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCodexArgumentsAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		spec Spec
		want []string
	}{
		{Spec{Provider: "codex", Batch: true, Prompt: "literal & prompt"}, []string{"--no-daemon", "exec", "--color", "never", "--", "literal & prompt"}},
		{Spec{Provider: "claude", Batch: true, Prompt: "literal & prompt"}, []string{"--print", "--", "literal & prompt"}},
		{Spec{Provider: "codex", Prompt: `--help; "quoted" & literal`}, []string{"--no-daemon", "--", `--help; "quoted" & literal`}},
		{Spec{Provider: "codex", Resume: "abc-123"}, []string{"--no-daemon", "resume", "abc-123"}},
		{Spec{Provider: "codex", Resume: "abc-123", Fork: true, Prompt: "continue"}, []string{"--no-daemon", "fork", "abc-123", "--", "continue"}},
		{Spec{Resume: "abc-123", Fork: true}, []string{"--resume", "abc-123", "--fork-session"}},
	} {
		if got := args(tc.spec); !slices.Equal(got, tc.want) {
			t.Fatalf("%q != %q", got, tc.want)
		}
	}
	if (Spec{Provider: "codex", Resume: "abc"}).SessionKey() != "codex:abc" {
		t.Fatal("missing provider namespace")
	}
	m := NewManager()
	if _, err := m.Start(Spec{Provider: "unknown", Dir: t.TempDir()}); err == nil {
		t.Fatal("unknown provider accepted")
	}
	tm := &Term{info: Info{ID: "owned", Provider: "codex"}}
	m.terms["owned"] = tm
	if !m.LinkCodex("owned", "codex:root") || m.LinkCodex("owned", "codex:child") || m.LinkCodex("missing", "codex:root") || tm.Info().SessionID != "codex:root" {
		t.Fatal("root association was not preserved")
	}
	m.terms["claude"] = &Term{info: Info{Provider: "claude"}}
	if m.LinkCodex("claude", "codex:root") {
		t.Fatal("cross-provider link")
	}
}

func TestNewAgentDoesNotInheritParentIdentity(t *testing.T) {
	for _, key := range []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID", "CODEX_APP_TOOLS_PIPE_PATH", "TRK_SESSION", "TRK_TERMINAL_ID", "CLAUDECODE"} {
		t.Setenv(key, "parent")
	}
	t.Setenv("CODEX_HOME", t.TempDir())
	env := CleanEnv()
	for _, kv := range env {
		if strings.HasSuffix(kv, "=parent") {
			t.Fatalf("leaked identity %s", kv)
		}
	}
	if !slices.Contains(env, "CODEX_HOME="+os.Getenv("CODEX_HOME")) {
		t.Fatal("lost user configuration")
	}
}
