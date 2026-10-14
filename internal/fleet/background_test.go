package fleet

import (
	"encoding/json"
	"github.com/GujaLomsadze/trk/internal/model"
	"testing"
)

func TestCodexSuggestionForksDoNotCreateCards(t *testing.T) {
	f := newFleet()
	now := int64(1000000)
	ingest(t, f, ch("main", "UserPromptSubmit", `,"prompt":"First message"`), now)
	ingest(t, f, ch("main", "Stop", ""), now+1)
	ingest(t, f, ch("main", "UserPromptSubmit", `,"prompt":"Second message"`), now+2)
	ingest(t, f, ch("suggestion", "SessionStart", `,"source":"fork"`), now+3)
	if len(f.View(now+3).Sessions) != 1 {
		t.Fatal("blank suggestion fork flashed a card")
	}
	prompt := `,"prompt":"This is an app-generated request for a suggested next user message. The user did not write this message. Return JSON."`
	ingest(t, f, ch("suggestion", "UserPromptSubmit", prompt), now+4)
	ingest(t, f, ch("suggestion", "Stop", `,"last_assistant_message":"a suggestion"`), now+5)
	ingest(t, f, ch("suggestion", "SessionEnd", ""), now+6)
	v := f.View(now + 7)
	if len(v.Sessions) != 1 || v.Stats.Agents != 1 || v.Sessions[0].LastPrompt != "Second message" {
		t.Fatalf("%+v", v)
	}
	// A live background suggestion must never capture directory-attributed progress.
	ingest(t, f, ch("suggestion", "UserPromptSubmit", prompt), now+8)
	e, _ := ingest(t, f, model.Envelope{Provider: "codex", Source: "cli", Kind: "step", Payload: json.RawMessage(`{"cwd":"/r/a","text":"testing"}`)}, now+9)
	if e.SessionID != "codex:main" {
		t.Fatalf("attribution: %s", e.SessionID)
	}
	// A real user fork in the same directory remains a separate card.
	ingest(t, f, ch("real-fork", "SessionStart", `,"source":"fork"`), now+10)
	ingest(t, f, ch("real-fork", "UserPromptSubmit", `,"prompt":"Implement another approach"`), now+11)
	if len(f.View(now+12).Sessions) != 2 {
		t.Fatal("real fork was hidden or merged")
	}
	// Stored summaries alone filter older suggestions outside the replay window.
	suggestion, _ := f.SessionRow("codex:suggestion")
	restarted := newFleet()
	restarted.Restore([]model.Session{suggestion}, model.Account{})
	if len(restarted.View(now+12).Sessions) != 0 {
		t.Fatal("suggestion reappeared after restart")
	}
	// Async completion can overtake the prompt; classification must still work.
	ingest(t, f, ch("late", "Stop", ""), now+20)
	late := ch("late", "UserPromptSubmit", prompt)
	late.TS = now + 19
	ingest(t, f, late, now+21)
	if len(f.View(now+22).Sessions) != 2 {
		t.Fatal("late suggestion prompt was not classified")
	}
}
