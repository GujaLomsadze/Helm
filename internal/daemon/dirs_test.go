package daemon

import (
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/GujaLomsadze/helm/internal/derive"
	"github.com/GujaLomsadze/helm/internal/fleet"
	"github.com/GujaLomsadze/helm/internal/gitinfo"
)

func TestSavedDirs(t *testing.T) {
	ts, srv := newTestServer(t)
	dir := derive.NormPath(t.TempDir())

	if r := postJSON(t, ts.URL+"/v1/dirs", `{"dir":"/no/such/dir","saved":true}`); r.StatusCode != 400 {
		t.Fatalf("missing folder = %d, want 400", r.StatusCode)
	}
	if r := postJSON(t, ts.URL+"/v1/dirs", `{"dir":`+jsonStr(dir)+`,"saved":true}`); r.StatusCode != 200 {
		t.Fatalf("save = %d", r.StatusCode)
	}
	postJSON(t, ts.URL+"/v1/dirs", `{"dir":`+jsonStr(dir)+`,"saved":true}`) // twice: still one entry
	var view struct {
		Saved []string `json:"saved_dirs"`
	}
	getJSON(t, ts.URL+"/v1/sessions", &view)
	if !slices.Equal(view.Saved, []string{dir}) {
		t.Fatalf("saved_dirs = %v, want [%s]", view.Saved, dir)
	}

	// survives a restart: a new server on the same store loads it
	again := NewServer(srv.st, fleet.New(gitinfo.Lookup), log.New(io.Discard, "", 0))
	if got := again.SavedDirs(); !slices.Equal(got, []string{dir}) {
		t.Fatalf("after restart = %v", got)
	}

	if r := postJSON(t, ts.URL+"/v1/dirs", `{"dir":`+jsonStr(dir)+`,"saved":false}`); r.StatusCode != 200 {
		t.Fatalf("unsave = %d", r.StatusCode)
	}
	getJSON(t, ts.URL+"/v1/sessions", &view)
	if len(view.Saved) != 0 {
		t.Fatalf("after unsave = %v", view.Saved)
	}
}

func TestCompleteDir(t *testing.T) {
	ts, _ := newTestServer(t)
	root := t.TempDir()
	for _, d := range []string{"repos", "Rust", ".hidden", "other"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "readme"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := derive.NormPath(root)
	complete := func(prefix string) []string {
		var out []string
		getJSON(t, ts.URL+"/v1/dirs/complete?prefix="+url.QueryEscape(prefix), &out)
		return out
	}
	if got, want := complete(r+"/r"), []string{r + "/Rust", r + "/repos"}; !slices.Equal(got, want) {
		t.Fatalf("complete r = %v, want %v (folders only, any case)", got, want)
	}
	if got := complete(r + "/"); len(got) != 3 {
		t.Fatalf("complete / = %v, want 3 folders without .hidden", got)
	}
	if got := complete(r + "/."); !slices.Equal(got, []string{r + "/.hidden"}) {
		t.Fatalf("complete . = %v", got)
	}
	if got := complete("relative/path"); len(got) != 0 {
		t.Fatalf("relative = %v, want none", got)
	}
}
