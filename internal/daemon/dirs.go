package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/GujaLomsadze/helm/internal/derive"
)

// Saved folders: the New agent form offers them on every start, even before any
// session has run there. Stored server-side so every browser sees the same list.

const (
	settingSavedDirs = "saved_dirs"
	maxSavedDirs     = 50
	maxCompletions   = 30
)

func (s *Server) loadSavedDirs() {
	v, ok, err := s.st.Setting(settingSavedDirs)
	if err != nil || !ok {
		return
	}
	var dirs []string
	if json.Unmarshal([]byte(v), &dirs) == nil {
		s.dirsMu.Lock()
		s.savedDirs = dirs
		s.dirsMu.Unlock()
	}
}

// SavedDirs returns a copy of the saved folders, newest first.
func (s *Server) SavedDirs() []string {
	s.dirsMu.Lock()
	defer s.dirsMu.Unlock()
	return append([]string{}, s.savedDirs...)
}

// expandHome turns a leading ~ into the user's home folder.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

// saveDir adds ({"dir": "/x", "saved": true}) or removes ({"saved": false}) a saved folder
// and answers with the new list.
func (s *Server) saveDir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dir   string `json:"dir"`
		Saved *bool  `json:"saved"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil || body.Saved == nil || strings.TrimSpace(body.Dir) == "" {
		http.Error(w, `want {"dir": "/path", "saved": bool}`, http.StatusBadRequest)
		return
	}
	dir := derive.NormPath(expandHome(strings.TrimSpace(body.Dir)))
	if *body.Saved {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() || !filepath.IsAbs(dir) {
			http.Error(w, "not a folder: "+dir, http.StatusBadRequest)
			return
		}
	}
	s.dirsMu.Lock()
	dirs := slices.DeleteFunc(append([]string{}, s.savedDirs...), func(d string) bool { return d == dir })
	if *body.Saved {
		dirs = append([]string{dir}, dirs...)
		if len(dirs) > maxSavedDirs {
			dirs = dirs[:maxSavedDirs]
		}
	}
	s.savedDirs = dirs
	b, _ := json.Marshal(dirs)
	err := s.st.SetSetting(settingSavedDirs, string(b)) // under the lock, so saves land in order
	s.dirsMu.Unlock()
	if err != nil {
		s.log.Printf("save setting: %v", err)
	}
	s.dirty.Store(true)
	writeJSON(w, dirs) // the new list, so the form shows the folder as the daemon stored it
}

// completeDir lists the folders that complete ?prefix= (e.g. "/home/me/re" → /home/me/repos).
// A prefix ending in "/" lists that folder's subfolders. Hidden folders show only when the
// typed name starts with a dot.
func (s *Server) completeDir(w http.ResponseWriter, r *http.Request) {
	prefix := expandHome(r.URL.Query().Get("prefix"))
	out := []string{}
	if !filepath.IsAbs(prefix) {
		writeJSON(w, out)
		return
	}
	prefix = strings.ReplaceAll(prefix, `\`, "/")
	i := strings.LastIndex(prefix, "/")
	parent, part := prefix[:i+1], strings.ToLower(prefix[i+1:])
	entries, err := os.ReadDir(parent)
	if err != nil {
		writeJSON(w, out)
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(part, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), part) {
			continue
		}
		if !e.IsDir() { // a symlink to a folder counts
			if fi, err := os.Stat(filepath.Join(parent, name)); err != nil || !fi.IsDir() {
				continue
			}
		}
		out = append(out, derive.NormPath(parent+name))
	}
	sort.Strings(out)
	if len(out) > maxCompletions {
		out = out[:maxCompletions]
	}
	writeJSON(w, out)
}
