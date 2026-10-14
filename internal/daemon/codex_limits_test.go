package daemon

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/GujaLomsadze/trk/internal/codex"
)

func TestCodexLimitsCacheAndStaleReadings(t *testing.T) {
	ts, srv := newTestServer(t)
	var calls atomic.Int32
	used := float64(7)
	srv.codexLimits.read = func(context.Context) ([]codex.LimitBucket, error) {
		calls.Add(1)
		return []codex.LimitBucket{{ID: "codex", Primary: &codex.LimitWindow{UsedPercent: &used}}}, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var v codexLimitsView
			getJSON(t, ts.URL+"/v1/codex/limits", &v)
			if v.Stale || v.UpdatedAt == 0 || len(v.Buckets) != 1 {
				t.Errorf("%+v", v)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("made %d upstream requests", calls.Load())
	}
	srv.codexLimits.mu.Lock()
	srv.codexLimits.at = 0
	srv.codexLimits.read = func(context.Context) ([]codex.LimitBucket, error) { calls.Add(1); return nil, errors.New("offline") }
	srv.codexLimits.mu.Unlock()
	for i := 0; i < 2; i++ {
		var v codexLimitsView
		getJSON(t, ts.URL+"/v1/codex/limits", &v)
		if !v.Stale || v.Error != "offline" || len(v.Buckets) != 1 || *v.Buckets[0].Primary.UsedPercent != 7 {
			t.Fatalf("%+v", v)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("failure wasn't cached")
	}
	if a := srv.fl.Account(); a.UpdatedAt != 0 || a.FiveHPct != nil {
		t.Fatal("Codex modified Claude account")
	}
	rows, _ := srv.st.Sessions()
	if len(rows) != 0 {
		t.Fatal("quota polling created session cards")
	}
}

func TestCodexLimitsSurvivesViewerDisconnect(t *testing.T) {
	_, srv := newTestServer(t)
	srv.codexLimits.read = func(ctx context.Context) ([]codex.LimitBucket, error) {
		if ctx.Err() != nil {
			t.Fatal("viewer cancellation reached the shared quota read")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("shared quota read must remain bounded")
		}
		return []codex.LimitBucket{{ID: "codex"}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/v1/codex/limits", nil).WithContext(ctx)
	srv.getCodexLimits(httptest.NewRecorder(), r)
	if srv.codexLimits.view.Stale || len(srv.codexLimits.view.Buckets) != 1 {
		t.Fatal("disconnected viewer poisoned the shared cache")
	}
}

func TestCodexLimitsRetryAndRecovery(t *testing.T) {
	_, srv := newTestServer(t)
	now := int64(100000)
	srv.now = func() int64 { return now }
	calls := 0
	srv.codexLimits.read = func(context.Context) ([]codex.LimitBucket, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("temporary failure")
		}
		return []codex.LimitBucket{{ID: "codex"}}, nil
	}
	get := func() {
		srv.getCodexLimits(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/codex/limits", nil))
	}
	get()
	now += 14999
	get()
	if calls != 1 {
		t.Fatal("retried too soon")
	}
	now++
	get()
	if calls != 2 || srv.codexLimits.view.Stale || srv.codexLimits.view.Error != "" {
		t.Fatal("did not recover after failure")
	}
	now += 15000
	get()
	if calls != 2 {
		t.Fatal("healthy reading lost its one-minute cache")
	}
}
