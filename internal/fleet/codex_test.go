package fleet

import (
	"encoding/json"
	"testing"

	"github.com/GujaLomsadze/trk/internal/model"
)

func ch(sid, event, extra string) model.Envelope {
	e := hook(sid, event, "/r/a", extra)
	e.Provider = "codex"
	return e
}

func TestCodexIdentityAndUnknownUsage(t *testing.T) {
	f := newFleet()
	now := int64(1000000)
	ingest(t, f, hook("same", "SessionStart", "/r/a", ""), now)
	ingest(t, f, ch("same", "SessionStart", `,"model":"test-model"`), now)
	if len(f.View(now).Sessions) != 2 {
		t.Fatal("providers merged")
	}
	if stats := f.View(now).Stats; stats.TokensKnown || !stats.TokensPartial {
		t.Fatalf("unknown tokens treated as measured: %+v", stats)
	}
	ingest(t, f, model.Envelope{Source: "api", Kind: "step", SessionID: "same", Payload: json.RawMessage(`{"text":"external progress"}`)}, now)
	claude, _ := f.SessionView("same", now)
	if claude.Provider != "claude" {
		t.Fatal("generic progress erased native provider")
	}
	v, ok := f.SessionView("codex:same", now)
	if !ok || v.Provider != "codex" || v.NativeID != "same" || v.CostKnown || v.TokensKnown || v.Capabilities.ManagedTerminal {
		t.Fatalf("%+v", v)
	}
	e, _ := ingest(t, f, model.Envelope{Provider: "codex", Source: "cli", Kind: "step", SessionID: "same", Payload: json.RawMessage(`{"text":"test"}`)}, now+1)
	if e.SessionID != "codex:same" {
		t.Fatal(e.SessionID)
	}
	// Two Codex sessions in a directory cannot be chosen reliably from cwd alone.
	ingest(t, f, ch("another", "SessionStart", ""), now+2)
	e, _ = ingest(t, f, model.Envelope{Provider: "codex", Source: "cli", Kind: "step", Payload: json.RawMessage(`{"cwd":"/r/a","text":"ambiguous"}`)}, now+3)
	if e.SessionID == "codex:same" || e.SessionID == "codex:another" {
		t.Fatal("guessed ambiguous session")
	}
}

func TestCodexOrderingInterruptAndPatchCollision(t *testing.T) {
	f := newFleet()
	now := int64(1000000)
	ingest(t, f, ch("s", "SessionStart", ""), now)
	extra := `,"turn_id":"t","tool_name":"Bash","tool_use_id":"call","tool_input":{"command":"test"}`
	post := ch("s", "PostToolUse", extra+`,"tool_response":{"exit_code":1}`)
	post.TS = now + 30
	ingest(t, f, post, now+40)
	pre := ch("s", "PreToolUse", extra)
	pre.TS = now + 10
	ingest(t, f, pre, now+40)
	perm := ch("s", "PermissionRequest", extra)
	perm.TS = now + 20
	ingest(t, f, perm, now+40)
	v, _ := f.SessionView("codex:s", now+40)
	if len(v.Calls) != 1 || !v.Calls[0].Failed || v.Need != nil {
		t.Fatalf("%+v", v)
	}
	ingest(t, f, ch("s", "PreToolUse", `,"tool_name":"apply_patch","tool_use_id":"patch","tool_input":{"command":"*** Update File: a.go\n*** Add File: b.go"}`), now+50)
	ingest(t, f, hook("claude", "PreToolUse", "/r/a", `,"tool_name":"Read","tool_input":{"file_path":"/r/a/b.go"}`), now+60)
	view := f.View(now + 70)
	if len(view.Collisions) != 1 || view.Collisions[0].Path != "/r/a/b.go" {
		t.Fatalf("%+v", view.Collisions)
	}
	v, _ = f.SessionView("codex:s", now+70)
	if v.FilesEdited != 2 {
		t.Fatalf("files: %d", v.FilesEdited)
	}
	ingest(t, f, ch("s", "PermissionRequest", extra), now+80)
	ingest(t, f, ch("s", "Interrupt", ""), now+90)
	v, _ = f.SessionView("codex:s", now+100)
	if v.Status != "interrupted" || v.Need != nil || v.Calls[1].End == 0 {
		t.Fatalf("%+v", v)
	}
	if len(f.View(now+100).Collisions) != 0 {
		t.Fatal("interrupted session still collides")
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit"} {
		late := ch("s", event, "")
		late.TS = now + 85
		ingest(t, f, late, now+100)
		v, _ = f.SessionView("codex:s", now+100)
		if v.Status != "interrupted" {
			t.Fatalf("late %s reopened turn: %s", event, v.Status)
		}
	}
	ingest(t, f, ch("s", "UserPromptSubmit", `,"prompt":"continue"`), now+110)
	v, _ = f.SessionView("codex:s", now+120)
	if v.Status != "working" {
		t.Fatal(v.Status)
	}
}
