package profile_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/GujaLomsadze/helm/internal/agent/all"
	"github.com/GujaLomsadze/helm/internal/profile"
)

func str(s string) *string { return &s }

func TestApplyValidatesAgainstPreset(t *testing.T) {
	in := profile.Input{Name: "Corp", Preset: "gateway",
		Values:  map[string]string{"ANTHROPIC_BASE_URL": "https://gw.example.com"},
		Secrets: map[string]*string{"ANTHROPIC_AUTH_TOKEN": str("sk-12345678abcd")}}
	p, sec, err := profile.Apply(nil, nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if p.Agent != "claude" || p.Spend != profile.SpendLocal || sec["ANTHROPIC_AUTH_TOKEN"] != "sk-12345678abcd" {
		t.Fatalf("%+v %v", p, sec)
	}
	if _, ok := p.Values["ANTHROPIC_AUTH_TOKEN"]; ok {
		t.Fatal("secret stored with the values")
	}

	// An update that leaves the key out keeps it; "" clears it (and it is required here).
	in.Secrets = nil
	if _, sec2, err := profile.Apply(&p, sec, in); err != nil || sec2["ANTHROPIC_AUTH_TOKEN"] != "sk-12345678abcd" {
		t.Fatalf("keep: %v %v", sec2, err)
	}
	in.Secrets = map[string]*string{"ANTHROPIC_AUTH_TOKEN": str("")}
	if _, _, err := profile.Apply(&p, sec, in); err == nil {
		t.Fatal("cleared a required key")
	}

	bad := []profile.Input{
		{Name: "", Preset: "gateway"},
		{Name: "x", Preset: "nope"},
		{Name: "x", Preset: "gateway", Values: map[string]string{"ANTHROPIC_BASE_URL": "ftp://gw"}, Secrets: map[string]*string{"ANTHROPIC_AUTH_TOKEN": str("k")}},
		{Name: "x", Preset: "anthropic-api", Spend: "litellm", Secrets: map[string]*string{"ANTHROPIC_API_KEY": str("k")}},
		{Name: "x", Preset: "custom", Extra: map[string]string{"BAD NAME": "1"}},
		{Name: "x", Preset: "custom", BudgetUSD: -1},
		{Name: "x", Preset: "custom", Color: "red;background:url(x)"},
		{Name: "x", Agent: "codex", Preset: "custom"},
	}
	for i, b := range bad {
		if _, _, err := profile.Apply(nil, nil, b); err == nil {
			t.Errorf("bad[%d] accepted", i)
		}
	}
}

func TestEnvAndEndpoint(t *testing.T) {
	p := profile.Profile{Agent: "claude", Preset: "bedrock-gateway",
		Values: map[string]string{"ANTHROPIC_BEDROCK_BASE_URL": "https://gw/bedrock"},
		Extra:  map[string]string{"ANTHROPIC_MODEL": "opus", "CLAUDE_CODE_SKIP_BEDROCK_AUTH": "0"}}
	env, err := profile.Env(p, map[string]string{"ANTHROPIC_AUTH_TOKEN": "tok"})
	if err != nil {
		t.Fatal(err)
	}
	want := "ANTHROPIC_AUTH_TOKEN=tok ANTHROPIC_BEDROCK_BASE_URL=https://gw/bedrock ANTHROPIC_MODEL=opus CLAUDE_CODE_SKIP_BEDROCK_AUTH=0 CLAUDE_CODE_USE_BEDROCK=1"
	if got := strings.Join(env, " "); got != want { // sorted; extra overrides the preset's fixed values
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if base, key, ok := profile.Endpoint(p, map[string]string{"ANTHROPIC_AUTH_TOKEN": "tok"}); !ok || base != "https://gw/bedrock" || key != "tok" {
		t.Fatal(base, key, ok)
	}
	if profile.Mask("sk-1234567890wxyz") != "…wxyz" || profile.Mask("short") != "…" {
		t.Fatal("mask")
	}
}

func TestSecretsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	s, err := profile.OpenSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("abc", map[string]string{"K": "v"}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", st.Mode().Perm())
		}
	}
	s2, err := profile.OpenSecrets(path)
	if err != nil || s2.Get("abc")["K"] != "v" {
		t.Fatal("not persisted", err)
	}
	_ = s2.Set("abc", nil)
	if s3, _ := profile.OpenSecrets(path); len(s3.Get("abc")) != 0 {
		t.Fatal("not removed")
	}
}

func TestLiteLLMSpend(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case r.Header.Get("Authorization") == "Bearer denied":
			http.Error(w, "no", http.StatusForbidden)
		case r.URL.Path == "/key/info" && r.Header.Get("Authorization") == "Bearer good":
			w.Write([]byte(`{"key":"x","info":{"spend":12.5,"max_budget":50,"budget_reset_at":"2026-12-01T00:00:00Z","key_alias":"me"}}`))
		case r.URL.Path == "/key/info":
			w.Write([]byte(`{"hello":"not litellm"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	now := time.UnixMilli(1)

	// The Anthropic route has a path; /key/info lives at the root.
	sp, err := profile.LiteLLMSpend(ctx, srv.URL+"/anthropic/", "good", now)
	if err != nil {
		t.Fatal(err)
	}
	if sp.SpendUSD != 12.5 || sp.BudgetUSD == nil || *sp.BudgetUSD != 50 || sp.Alias != "me" ||
		sp.ResetAt != time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("%+v", sp)
	}
	if strings.Join(paths, " ") != "/anthropic/key/info /key/info" {
		t.Fatal(paths)
	}
	if _, err := profile.LiteLLMSpend(ctx, srv.URL, "denied", now); !errors.Is(err, profile.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := profile.LiteLLMSpend(ctx, srv.URL, "other", now); !errors.Is(err, profile.ErrNotLiteLLM) {
		t.Fatal(err)
	}
}
