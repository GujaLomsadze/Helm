package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GujaLomsadze/trk/internal/fleet"
	"github.com/GujaLomsadze/trk/internal/gitinfo"
	"github.com/GujaLomsadze/trk/internal/schedule"
	"github.com/GujaLomsadze/trk/internal/store"
	"github.com/GujaLomsadze/trk/internal/term"
)

func scheduleServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "trk.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(st, fleet.New(gitinfo.Lookup), log.New(io.Discard, "", 0))
	t.Cleanup(func() { s.CloseTerminals(); st.Close() })
	return s
}

func scheduleRequest(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(string(b)))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func testSchedule(t *testing.T, s *Server, provider, prompt string) schedule.Job {
	t.Helper()
	c := schedule.Config{Name: "Scheduled test", Provider: provider, Prompt: prompt, Dir: t.TempDir(), Repeat: "once", RunAt: s.now() + 3600000, Timezone: "Europe/Berlin", TimeoutMinutes: 1}
	w := scheduleRequest(t, s, "POST", "/v1/schedules", c)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var j schedule.Job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestScheduleAPIValidationPauseEditAndDelete(t *testing.T) {
	s := scheduleServer(t)
	j := testSchedule(t, s, "codex", "literal <script>alert(1)</script>")
	if w := scheduleRequest(t, s, "POST", "/v1/schedules/"+j.ID+"/run", nil); w.Code != http.StatusForbidden {
		t.Fatalf("bypassed terminals setting: %d", w.Code)
	}
	if w := scheduleRequest(t, s, "POST", "/v1/schedules/"+j.ID+"/enabled", map[string]bool{"enabled": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	j.Config.Name = "Updated"
	if w := scheduleRequest(t, s, "PUT", "/v1/schedules/"+j.ID, j.Config); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	stored, err := s.findSchedule(j.ID)
	if err != nil || stored.Enabled || stored.Name != "Updated" {
		t.Fatal(stored, err)
	}
	j.Config.Dir = "relative"
	if w := scheduleRequest(t, s, "POST", "/v1/schedules", j.Config); w.Code != 400 {
		t.Fatal("relative folder accepted")
	}
	if w := scheduleRequest(t, s, "DELETE", "/v1/schedules/"+j.ID, nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := scheduleRequest(t, s, "GET", "/v1/schedules/"+j.ID+"/runs", nil); w.Code != 404 {
		t.Fatal("missing history returned", w.Code)
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1/v1/schedules", nil)
	r.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("foreign origin accepted")
	}
}

func TestScheduledBatchResultsForBothProviders(t *testing.T) {
	if !term.Supported {
		t.Skip("PTY unavailable")
	}
	t.Setenv("TRK_TEST_SCHEDULE_HELPER", "1")
	for _, provider := range []string{"claude", "codex"} {
		for _, prompt := range []string{`literal & "quoted"`, "fail"} {
			t.Run(provider+prompt, func(t *testing.T) {
				s := scheduleServer(t)
				s.SetTerminals(true)
				s.terms.Command = os.Args[0]
				s.terms.CodexCommand = os.Args[0]
				j := testSchedule(t, s, provider, prompt)
				j.NextRunAt = s.now() - 60000
				if err := s.st.SaveSchedule(j, false); err != nil {
					t.Fatal(err)
				}
				s.scheduleTick()
				r := waitSchedule(t, s, j.ID)
				want := 0
				if prompt == "fail" {
					want = 7
				}
				if r.ExitCode == nil || *r.ExitCode != want || (r.Status == "succeeded") != (want == 0) || !strings.Contains(r.Output, "SCHEDULE-RESULT:"+prompt) {
					t.Fatalf("bad result: %+v", r)
				}
				s.scheduleTick()
				runs, _ := s.st.ScheduleRuns(j.ID, true)
				if len(runs) != 1 {
					t.Fatal("one-time task ran more than once")
				}
				if len(s.terms.List()) != 0 {
					t.Fatal("completed scheduled terminal leaked")
				}
			})
		}
	}
}

func waitSchedule(t *testing.T, s *Server, id string) schedule.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.scheduleTick()
		runs, err := s.st.ScheduleRuns(id, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) > 0 && runs[0].Status != "running" {
			return runs[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scheduled run did not finish")
	return schedule.Run{}
}

func TestScheduleNoOverlapPauseAndTimeout(t *testing.T) {
	if !term.Supported {
		t.Skip("PTY unavailable")
	}
	t.Setenv("TRK_TEST_SCHEDULE_HELPER", "1")
	s := scheduleServer(t)
	s.SetTerminals(true)
	s.terms.CodexCommand = os.Args[0]
	now := time.Now().UnixMilli()
	s.now = func() int64 { return now }
	j := testSchedule(t, s, "codex", "hold")
	j.Repeat = "interval"
	j.EveryMinutes = 1
	j.RunAt = now - 120000
	j.NextRunAt = j.RunAt
	if err := s.st.SaveSchedule(j, false); err != nil {
		t.Fatal(err)
	}
	s.scheduleTick()
	// A concurrent manual run cannot append to this running occurrence.
	if w := scheduleRequest(t, s, "POST", "/v1/schedules/"+j.ID+"/run", nil); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := scheduleRequest(t, s, "DELETE", "/v1/schedules/"+j.ID, nil); w.Code != 409 {
		t.Fatal("deleted active task")
	}
	if w := scheduleRequest(t, s, "POST", "/v1/schedules/"+j.ID+"/enabled", map[string]bool{"enabled": false}); w.Code != 200 {
		t.Fatal("cannot pause active task", w.Body.String())
	}
	now += 180000
	s.scheduleTick()
	r := waitSchedule(t, s, j.ID)
	if r.Status != "stopped" || !strings.Contains(r.Error, "time limit") {
		t.Fatalf("timeout: %+v", r)
	}
	runs, _ := s.st.ScheduleRuns(j.ID, true)
	if len(runs) != 1 {
		t.Fatal("overlapped or replayed missed runs")
	}
	if w := scheduleRequest(t, s, "POST", "/v1/schedules/"+j.ID+"/enabled", map[string]bool{"enabled": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	stored, _ := s.findSchedule(j.ID)
	if stored.NextRunAt <= now {
		t.Fatal("resume replayed paused occurrences")
	}
}

func TestScheduledLaunchFailureIsNotRetried(t *testing.T) {
	s := scheduleServer(t)
	s.SetTerminals(true)
	s.terms.CodexCommand = filepath.Join(t.TempDir(), "missing-agent.exe")
	j := testSchedule(t, s, "codex", "hello")
	j.NextRunAt = s.now() - 1
	if err := s.st.SaveSchedule(j, false); err != nil {
		t.Fatal(err)
	}
	s.scheduleTick()
	s.scheduleTick()
	runs, err := s.st.ScheduleRuns(j.ID, true)
	if err != nil || len(runs) != 1 || runs[0].Status != "failed" || runs[0].Error == "" {
		t.Fatalf("%+v %v", runs, err)
	}
}
