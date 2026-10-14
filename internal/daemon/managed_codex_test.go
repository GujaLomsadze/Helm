package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GujaLomsadze/trk/internal/term"
	"github.com/gorilla/websocket"
)

// A fake interactive agent exercises the real native terminal and HTTP bridge.
func TestMain(m *testing.M) {
	if os.Getenv("TRK_TEST_AGENT_HELPER") == "1" {
		fmt.Println("AGENT-READY")
		s := bufio.NewScanner(os.Stdin)
		for s.Scan() {
			fmt.Println("AGENT-REPLY:" + s.Text())
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestManagedCodexHTTP(t *testing.T) {
	if !term.Supported {
		t.Skip("PTY unavailable")
	}
	t.Setenv("TRK_TEST_AGENT_HELPER", "1")
	ts, srv := newTestServer(t)
	t.Cleanup(srv.CloseTerminals)
	srv.terms.CodexCommand = os.Args[0]
	sp := term.Spec{Provider: "codex", Dir: t.TempDir(), Title: "Codex test"}
	request := func(spec term.Spec) *http.Response {
		b, _ := json.Marshal(spec)
		r, err := http.Post(ts.URL+"/v1/terms", "application/json", strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := request(sp)
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatal("terminals bypassed enable setting")
	}
	srv.SetTerminals(true)
	r = request(sp)
	if r.StatusCode != http.StatusCreated {
		t.Fatal(r.Status)
	}
	var info term.Info
	_ = json.NewDecoder(r.Body).Decode(&info)
	r.Body.Close()
	if info.Provider != "codex" || info.PID == 0 {
		t.Fatalf("%+v", info)
	}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/v1/terms/"+info.ID+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.WriteMessage(websocket.BinaryMessage, []byte("ping-trk\r\n"))
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	var output strings.Builder
	for !strings.Contains(output.String(), "AGENT-REPLY:ping-trk") {
		_, b, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		output.Write(b)
	}
	event := fmt.Sprintf(`{"provider":"codex","source":"hook","terminal_id":%q,"payload":{"session_id":"root","hook_event_name":"SessionStart","cwd":%q}}`, info.ID, sp.Dir)
	if post(t, ts.URL, event, nil) != http.StatusAccepted {
		t.Fatal("hook rejected")
	}
	var view dashView
	getJSON(t, ts.URL+"/v1/sessions", &view)
	if view.Running["codex:root"] != info.ID || len(view.Terminals) != 1 || view.Terminals[0].SessionID != "codex:root" {
		t.Fatalf("not linked: %+v", view.Terminals)
	}
	row, _ := srv.fl.SessionRow("codex:root")
	if row.Title != "Codex test" {
		t.Fatalf("title=%q", row.Title)
	}
	sp.Resume = "root"
	r = request(sp)
	r.Body.Close()
	if r.StatusCode != http.StatusConflict {
		t.Fatal("duplicate managed resume allowed")
	}
	event = strings.Replace(event, `"session_id":"root"`, `"session_id":"child"`, 1)
	post(t, ts.URL, event, nil)
	getJSON(t, ts.URL+"/v1/terms", &struct{}{})
	if srv.terms.List()[0].SessionID != "codex:root" {
		t.Fatal("subagent replaced root")
	}
	req, _ := http.NewRequest("DELETE", ts.URL+"/v1/terms/"+info.ID, nil)
	r, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusNoContent || srv.terms.Running() != 0 {
		t.Fatal("stop failed")
	}
}
