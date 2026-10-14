package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/GujaLomsadze/trk/internal/model"
)

func TestProviderMigrationAndDedup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "previous.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:5] {
		if _, err = db.Exec(migration); err != nil {
			t.Fatal(err)
		}
	}
	for _, stmt := range []string{`PRAGMA user_version=5`,
		`INSERT INTO sessions(session_id,name,title) VALUES('s','old','My title')`,
		`INSERT INTO events(ts,session_id,source,kind,payload) VALUES(1,'s','statusline','status','{"cost":{"total_cost_usd":0},"context_window":{"total_input_tokens":10}}')`,
		`INSERT INTO settings VALUES('hide_after_min','60')`} {
		if _, err = db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.Sessions()
	if err != nil || len(rows) != 1 || rows[0].Provider != "claude" || !rows[0].CostKnown || !rows[0].TokensKnown || rows[0].Title != "My title" {
		t.Fatalf("%+v %v", rows, err)
	}
	e := model.Event{Provider: "codex", NativeID: "s", SessionID: "codex:s", EventKey: "key", Source: "hook", Kind: "tool_post", TS: 2, Payload: json.RawMessage(`{}`)}
	if err = s.Append(&e); err != nil {
		t.Fatal(err)
	}
	if err = s.Append(&e); !errors.Is(err, ErrDuplicate) {
		t.Fatal(err)
	}
	evs, err := s.EventsSince(0)
	if err != nil || len(evs) != 2 || evs[1].NativeID != "s" {
		t.Fatalf("%+v %v", evs, err)
	}
	if v, _, _ := s.Setting("hide_after_min"); v != "60" {
		t.Fatal("lost setting")
	}
}

func TestTokenOnlyStatusBaseline(t *testing.T) {
	s, _ := open(t)
	for i, raw := range []string{`{"context_window":{"total_input_tokens":100}}`, `{"context_window":{"total_input_tokens":150}}`} {
		if err := s.Append(&model.Event{SessionID: "codex:s", Provider: "codex", Source: "api", Kind: "status", TS: int64(i + 1), Payload: json.RawMessage(raw)}); err != nil {
			t.Fatal(err)
		}
	}
	r, ok, err := s.LastStatus("codex:s", 2)
	if err != nil || !ok || !r.HasTokens || r.HasCost || r.Tokens != 100 {
		t.Fatalf("%+v %v %v", r, ok, err)
	}
}
