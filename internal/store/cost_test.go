package store

import (
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"testing"
)

// Upgrading fills the cost ledger from the status lines already stored: growth per hour,
// with a drop in the counter counted as a new process.
func TestCostBackfillOnUpgrade(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hlm.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	last := len(migrations) - 1
	for _, m := range migrations[:last] {
		if _, err := db.Exec(m); err != nil {
			t.Fatal(err)
		}
	}
	h := int64(hourMs)
	ins := func(ts int64, sid string, cost float64) {
		q := fmt.Sprintf(`INSERT INTO events(ts, session_id, source, kind, payload) VALUES(%d,'%s','statusline','status','{"cost":{"total_cost_usd":%g}}')`, ts, sid, cost)
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ins(10*h+1, "a", 1)
	ins(10*h+2, "a", 1.5)
	ins(11*h+1, "a", 4)   // +2.5 in hour 11
	ins(11*h+2, "a", 0.5) // resumed: a new process from 0
	ins(10*h+5, "b", 2)
	if _, err := db.Exec(`INSERT INTO events(ts, session_id, source, kind, payload) VALUES(1,'a','hook','prompt','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, last)); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.CostHours(0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, r := range rows {
		got[fmt.Sprintf("%s@%d", r.Session, r.Hour/h)] = r.USD
	}
	want := map[string]float64{"a@10": 1.5, "a@11": 3, "b@10": 2}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if math.Abs(got[k]-v) > 1e-9 {
			t.Fatalf("%s = %v, want %v (all %v)", k, got[k], v, got)
		}
	}

	if err := s.AddCost("b", 10*h+9, 0.25); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.CostHours(10*h + 30) // from the hour containing it
	for _, r := range rows {
		if r.Session == "b" && math.Abs(r.USD-2.25) > 1e-9 {
			t.Fatalf("b = %v", r.USD)
		}
	}
	if c, ok, _ := s.LastCost("a", 11*h+2); !ok || c != 4 {
		t.Fatal(c, ok)
	}
}

func TestSessionProfiles(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hlm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.SetSessionProfile("s1", "p1", 1)
	_ = s.SetSessionProfile("s2", "p1", 1)
	_ = s.SetSessionProfile("s1", "p2", 2)
	_ = s.SetSessionProfile("s2", "", 3)
	m, err := s.SessionProfiles()
	if err != nil || len(m) != 1 || m["s1"] != "p2" {
		t.Fatal(m, err)
	}
}
