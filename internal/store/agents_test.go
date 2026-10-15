package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GujaLomsadze/helm/internal/model"
)

func TestUpgradeTagsClaudeHistory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hlm.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:len(migrations)-1] { // the schema before agents existed
		if _, err := db.Exec(m); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations)-1),
		`INSERT INTO events(ts, session_id, source, kind, payload) VALUES(1,'c1','hook','prompt','{}'),(2,'c1','cli','step','{}'),(3,'x','cli','step','{}')`,
		`INSERT INTO sessions(session_id, title) VALUES('c1','kept'),('x','')`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	db.Close()

	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ss, _ := s.Sessions()
	got := map[string]model.Session{}
	for _, x := range ss {
		got[x.SessionID] = x
	}
	if c := got["c1"]; c.Agent != "claude" || c.NativeID != "c1" || c.Title != "kept" {
		t.Fatalf("%+v", c)
	}
	if x := got["x"]; x.Agent != "" {
		t.Fatalf("CLI-only session got an agent: %+v", x)
	}
	evs, _ := s.EventsSince(0)
	if evs[0].Agent != "claude" || evs[1].Agent != "" {
		t.Fatalf("%+v", evs)
	}
}

func TestAppendDedupesByKey(t *testing.T) {
	s, _ := open(t)
	e := model.Event{TS: 1, SessionID: "codex:a", Source: "hook", Kind: "tool_pre", Payload: json.RawMessage(`{}`), Agent: "codex", NativeID: "a", Key: "k1"}
	if err := s.Append(&e); err != nil {
		t.Fatal(err)
	}
	dup := e
	if err := s.Append(&dup); !errors.Is(err, ErrDuplicate) {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // no key: never a duplicate
		n := model.Event{TS: 2, SessionID: "c", Source: "hook", Kind: "stop", Payload: json.RawMessage(`{}`)}
		if err := s.Append(&n); err != nil {
			t.Fatal(err)
		}
	}
	evs, _ := s.EventsSince(0)
	if len(evs) != 3 || evs[0].Agent != "codex" || evs[0].NativeID != "a" || evs[0].Key != "k1" {
		t.Fatalf("%+v", evs)
	}
}

func TestSessionAgentAndAgentsSeen(t *testing.T) {
	s, _ := open(t)
	s.UpsertSession(model.Session{SessionID: "codex:a", Agent: "codex", NativeID: "a", Background: true, LastEventAt: 500})
	s.UpsertSession(model.Session{SessionID: "c", Agent: "claude", LastEventAt: 50})
	ss, _ := s.Sessions()
	for _, x := range ss {
		if x.SessionID == "codex:a" && (!x.Background || x.NativeID != "a") {
			t.Fatalf("%+v", x)
		}
	}
	if a, err := s.Agents(100); err != nil || strings.Join(a, ",") != "codex" {
		t.Fatal(a, err)
	}
}

func TestStatRowsReadToolFiles(t *testing.T) {
	s, _ := open(t)
	e := model.Event{TS: 5, SessionID: "x", Source: "hook", Kind: "tool_pre", Payload: json.RawMessage(`{"tool_name":"apply_patch","tool_files":["a.go","b.go"],"tool_kind":"edit"}`)}
	s.Append(&e)
	rows, err := s.StatRows(0, 10)
	if err != nil || len(rows) != 1 || len(rows[0].Files) != 2 || rows[0].ToolKind != "edit" {
		t.Fatalf("%+v %v", rows, err)
	}
}
