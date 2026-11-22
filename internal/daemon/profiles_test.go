//go:build !windows

package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func doJSON(t *testing.T, method, url, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

const corpProfile = `{"name":"Corp","preset":"gateway","values":{"ANTHROPIC_BASE_URL":"https://gw.example.com"},
"secrets":{"ANTHROPIC_AUTH_TOKEN":"sk-secret-corp-1234"},"extra":{"HELM_TEST_EXTRA":"yes"},"budget_usd":50,"budget_days":30}`

func TestProfilesNeverReturnKeys(t *testing.T) {
	ts, srv := newTestServer(t)
	if err := srv.OpenSecrets(filepath.Join(t.TempDir(), "secrets.json")); err != nil {
		t.Fatal(err)
	}
	r, body := doJSON(t, "POST", ts.URL+"/v1/profiles", corpProfile)
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %s", r.StatusCode, body)
	}
	var p struct{ ID string }
	_ = json.Unmarshal([]byte(body), &p)
	if r, _ := doJSON(t, "POST", ts.URL+"/v1/profiles", corpProfile); r.StatusCode != http.StatusConflict {
		t.Fatalf("same name = %d", r.StatusCode)
	}
	for _, u := range []string{"/v1/profiles", "/v1/sessions"} {
		_, out := doJSON(t, "GET", ts.URL+u, "")
		if strings.Contains(out, "sk-secret-corp") || strings.Contains(body, "sk-secret-corp") {
			t.Fatalf("%s leaks the key: %s", u, out)
		}
		if !strings.Contains(out, `"ANTHROPIC_AUTH_TOKEN":true`) {
			t.Fatalf("%s doesn't say the key is set: %s", u, out)
		}
	}
	// Rename without resending the key: it stays.
	upd := strings.Replace(corpProfile, `"secrets":{"ANTHROPIC_AUTH_TOKEN":"sk-secret-corp-1234"},`, "", 1)
	upd = strings.Replace(upd, `"Corp"`, `"Corp 2"`, 1)
	if r, b := doJSON(t, "PUT", ts.URL+"/v1/profiles/"+p.ID, upd); r.StatusCode != http.StatusOK {
		t.Fatalf("update = %d %s", r.StatusCode, b)
	}
	if _, sec, _ := srv.profileByID(p.ID); sec["ANTHROPIC_AUTH_TOKEN"] != "sk-secret-corp-1234" {
		t.Fatal("key lost on update")
	}
	if r, _ := doJSON(t, "DELETE", ts.URL+"/v1/profiles/"+p.ID, ""); r.StatusCode != http.StatusNoContent {
		t.Fatal(r.StatusCode)
	}
	if _, sec, ok := srv.profileByID(p.ID); ok || len(sec) != 0 || len(srv.prof.secrets.Get(p.ID)) != 0 {
		t.Fatal("not deleted")
	}
}

// A terminal started with a profile gets its variables and none of the shell's own
// provider settings; resuming that session keeps the profile.
func TestTerminalRunsWithProfile(t *testing.T) {
	ts, srv := newTestServer(t)
	t.Cleanup(srv.CloseTerminals)
	t.Setenv("ANTHROPIC_API_KEY", "from-the-shell")
	fake := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nenv | grep -E '^(ANTHROPIC_|HELM_TEST_)' | sort | sed 's/^/ENV /'\necho READY\nexec cat\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	srv.terms.Command = fake
	_, body := doJSON(t, "POST", ts.URL+"/v1/profiles", corpProfile)
	var p struct{ ID string }
	_ = json.Unmarshal([]byte(body), &p)
	dir := t.TempDir()

	r, body := doJSON(t, "POST", ts.URL+"/v1/terms", `{"dir":`+jsonStr(dir)+`,"profile":"`+p.ID+`"}`)
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("start = %d %s", r.StatusCode, body)
	}
	var info struct{ ID, Profile string }
	_ = json.Unmarshal([]byte(body), &info)
	if info.Profile != p.ID {
		t.Fatalf("terminal profile %q", info.Profile)
	}
	out := termOutput(t, srv, info.ID)
	for _, want := range []string{"ENV ANTHROPIC_AUTH_TOKEN=sk-secret-corp-1234", "ENV ANTHROPIC_BASE_URL=https://gw.example.com", "ENV HELM_TEST_EXTRA=yes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "from-the-shell") {
		t.Fatalf("the shell's key leaked into a profile terminal:\n%s", out)
	}

	// The session's hooks report from the terminal: it is tagged with the profile.
	post(t, ts.URL, fmt.Sprintf(`{"source":"hook","terminal_id":%q,"payload":{"session_id":"S-corp","hook_event_name":"SessionStart","cwd":%s}}`, info.ID, jsonStr(dir)), nil)
	waitFor(t, func() bool { return srv.sessionProfiles(nil)["S-corp"] == p.ID }, "session tagged")
	var view struct {
		SessionProfiles map[string]string `json:"session_profiles"`
	}
	getJSON(t, ts.URL+"/v1/sessions", &view)
	if view.SessionProfiles["S-corp"] != p.ID {
		t.Fatalf("%+v", view)
	}

	// A plain terminal still gets the shell's environment.
	_, body = doJSON(t, "POST", ts.URL+"/v1/terms", `{"dir":`+jsonStr(dir)+`}`)
	var plain struct{ ID string }
	_ = json.Unmarshal([]byte(body), &plain)
	if out := termOutput(t, srv, plain.ID); !strings.Contains(out, "ANTHROPIC_API_KEY=from-the-shell") {
		t.Fatalf("plain terminal lost the shell env:\n%s", out)
	}

	// Resume without naming a profile: the session's own.
	srv.terms.Stop(info.ID)
	waitFor(t, func() bool {
		r, b := doJSON(t, "POST", ts.URL+"/v1/terms", `{"dir":`+jsonStr(dir)+`,"resume":"S-corp"}`)
		if r.StatusCode != http.StatusCreated {
			return false
		}
		var ri struct{ Profile string }
		_ = json.Unmarshal([]byte(b), &ri)
		if ri.Profile != p.ID {
			t.Fatalf("resume profile %q", ri.Profile)
		}
		return true
	}, "resume")

	if r, _ := doJSON(t, "POST", ts.URL+"/v1/terms", `{"dir":`+jsonStr(dir)+`,"profile":"aaaaaaaaaaaa"}`); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown profile = %d", r.StatusCode)
	}
}

func termOutput(t *testing.T, srv *Server, id string) string {
	t.Helper()
	tm, ok := srv.terms.Get(id)
	if !ok {
		t.Fatal("no terminal", id)
	}
	var out string
	waitFor(t, func() bool {
		snap, _, detach := tm.Attach()
		detach()
		out = string(snap)
		return strings.Contains(out, "READY")
	}, "terminal output")
	return out
}

func TestCostPerProfile(t *testing.T) {
	ts, srv := newTestServer(t)
	_, body := doJSON(t, "POST", ts.URL+"/v1/profiles", corpProfile)
	var p struct{ ID string }
	_ = json.Unmarshal([]byte(body), &p)
	srv.tagSession("S-corp", p.ID)
	status := func(sid string, cost float64) {
		post(t, ts.URL, fmt.Sprintf(`{"source":"statusline","payload":{"session_id":%q,"cwd":"/tmp/p","cost":{"total_cost_usd":%g}}}`, sid, cost), nil)
	}
	status("S-corp", 1)
	status("S-corp", 3.5)
	status("S-mine", 2)
	status("S-corp", 0.5) // resumed: a new process counts from zero

	var res struct {
		Profiles []struct {
			ID        string  `json:"id"`
			Name      string  `json:"name"`
			RangeUSD  float64 `json:"range_usd"`
			WindowUSD float64 `json:"window_usd"`
			BudgetUSD float64 `json:"budget_usd"`
			Sessions  int     `json:"sessions"`
		} `json:"profiles"`
		Series []struct {
			Profile string  `json:"profile"`
			USD     float64 `json:"usd"`
		} `json:"series"`
	}
	getJSON(t, ts.URL+"/v1/cost?days=7", &res)
	got := map[string]float64{}
	for _, cp := range res.Profiles {
		got[cp.ID] = cp.RangeUSD
		if cp.ID == p.ID && (math.Abs(cp.WindowUSD-4) > 1e-9 || cp.BudgetUSD != 50 || cp.Sessions != 1) {
			t.Fatalf("%+v", cp)
		}
	}
	if math.Abs(got[p.ID]-4) > 1e-9 || math.Abs(got[""]-2) > 1e-9 {
		t.Fatalf("%+v", res.Profiles)
	}
	var sum float64
	for _, pt := range res.Series {
		sum += pt.USD
	}
	if math.Abs(sum-6) > 1e-9 {
		t.Fatalf("series sum %v", sum)
	}
	if r, _ := doJSON(t, "GET", ts.URL+"/v1/cost?days=0", ""); r.StatusCode != http.StatusBadRequest {
		t.Fatal(r.StatusCode)
	}
}
