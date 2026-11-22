package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/GujaLomsadze/helm/internal/agent"
	"github.com/GujaLomsadze/helm/internal/profile"
	"github.com/GujaLomsadze/helm/internal/term"
)

// Launch profiles: named provider setups (a company gateway, an API key, a cloud account)
// the New agent form can start an agent with. The list lives in the settings table; keys
// live in a 0600 file next to the database and are never sent back to the dashboard.

const settingProfiles = "profiles"

type profileState struct {
	mu       sync.Mutex
	list     []profile.Profile
	secrets  *profile.Secrets
	sessions map[string]string // session → profile id
}

func (s *Server) loadProfiles() {
	s.prof.secrets, _ = profile.OpenSecrets("")
	s.prof.sessions = map[string]string{}
	if v, ok, err := s.st.Setting(settingProfiles); err == nil && ok {
		_ = json.Unmarshal([]byte(v), &s.prof.list)
	}
	if m, err := s.st.SessionProfiles(); err == nil {
		s.prof.sessions = m
	}
}

// OpenSecrets points the key store at its file; Run calls it with the data folder.
func (s *Server) OpenSecrets(path string) error {
	sec, err := profile.OpenSecrets(path)
	if err != nil {
		return err
	}
	s.prof.mu.Lock()
	s.prof.secrets = sec
	s.prof.mu.Unlock()
	return nil
}

func (s *Server) saveProfilesLocked() error {
	b, err := json.Marshal(s.prof.list)
	if err != nil {
		return err
	}
	return s.st.SetSetting(settingProfiles, string(b))
}

func (s *Server) findLocked(id string) (int, bool) {
	for i, p := range s.prof.list {
		if p.ID == id {
			return i, true
		}
	}
	return -1, false
}

// publicProfiles is the list the dashboard gets: which keys are set, never their values.
func (s *Server) publicProfiles() []profile.Public {
	s.prof.mu.Lock()
	defer s.prof.mu.Unlock()
	out := make([]profile.Public, 0, len(s.prof.list))
	for _, p := range s.prof.list {
		set := map[string]bool{}
		for k, v := range s.prof.secrets.Get(p.ID) {
			set[k] = v != ""
		}
		out = append(out, profile.Public{Profile: p, Secrets: set})
	}
	return out
}

func (s *Server) profileByID(id string) (profile.Profile, map[string]string, bool) {
	s.prof.mu.Lock()
	defer s.prof.mu.Unlock()
	i, ok := s.findLocked(id)
	if !ok {
		return profile.Profile{}, nil, false
	}
	return s.prof.list[i], s.prof.secrets.Get(id), true
}

// sessionProfiles maps the given sessions (nil = all) to the profile they ran with.
func (s *Server) sessionProfiles(only map[string]bool) map[string]string {
	s.prof.mu.Lock()
	defer s.prof.mu.Unlock()
	out := map[string]string{}
	for sid, pid := range s.prof.sessions {
		if only == nil || only[sid] {
			out[sid] = pid
		}
	}
	return out
}

// tagSession records that a session runs with a profile ("" = none).
func (s *Server) tagSession(sid, pid string) {
	if sid == "" {
		return
	}
	s.prof.mu.Lock()
	if s.prof.sessions[sid] == pid {
		s.prof.mu.Unlock()
		return
	}
	if pid == "" {
		delete(s.prof.sessions, sid)
	} else {
		s.prof.sessions[sid] = pid
	}
	s.prof.mu.Unlock()
	if err := s.st.SetSessionProfile(sid, pid, s.now()); err != nil {
		s.log.Printf("session profile: %v", err)
	}
	s.dirty.Store(true)
}

// resolveProfile fills sp.Env and sp.Scrub from the profile the request names. Left out on
// a resume, it is the profile the session ran with, so a company session never resumes on
// a personal login (and the other way round).
func (s *Server) resolveProfile(sp *term.Spec) error {
	if sp.Profile == nil {
		pid := ""
		if sp.Resume != "" {
			pid = s.sessionProfiles(nil)[agent.SessionKey(sp.Agent, sp.Resume)]
		}
		sp.Profile = &pid
	}
	if *sp.Profile == "" {
		return nil
	}
	p, sec, ok := s.profileByID(*sp.Profile)
	if !ok {
		return errors.New("that launch profile no longer exists: pick another")
	}
	if p.Agent != sp.Agent {
		return errors.New("that launch profile is for another agent")
	}
	env, err := profile.Env(p, sec)
	if err != nil {
		return err
	}
	_, pr, err := profile.PresetOf(p.Agent, p.Preset)
	if err != nil {
		return err
	}
	sp.Env, sp.Scrub = env, pr.ProfileScrub
	return nil
}

type presetsInfo struct {
	Agent     string         `json:"agent"`
	Presets   []agent.Preset `json:"presets"`
	Conflicts []string       `json:"conflicts,omitempty"`
}

// listProfiles: GET /v1/profiles → the profiles and, per agent that supports them, its
// presets and any settings that would override a profile.
func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	var presets []presetsInfo
	for _, a := range agent.All() {
		if pr, ok := a.(agent.Profiler); ok {
			presets = append(presets, presetsInfo{Agent: a.Name(), Presets: pr.Presets(), Conflicts: pr.ProfileConflicts()})
		}
	}
	writeJSON(w, map[string]any{"profiles": s.publicProfiles(), "presets": presets})
}

// putProfile creates (POST /v1/profiles) or updates (PUT /v1/profiles/{id}) a profile.
func (s *Server) putProfile(w http.ResponseWriter, r *http.Request) {
	var in profile.Input
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	s.prof.mu.Lock()
	defer s.prof.mu.Unlock()
	var prev *profile.Profile
	idx := -1
	if id != "" {
		i, ok := s.findLocked(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		idx, prev = i, &s.prof.list[i]
	} else if len(s.prof.list) >= profile.MaxProfiles {
		http.Error(w, "too many launch profiles", http.StatusBadRequest)
		return
	}
	var prevSec map[string]string
	if prev != nil {
		prevSec = s.prof.secrets.Get(id)
	}
	p, sec, err := profile.Apply(prev, prevSec, in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if prev == nil {
		p.ID, p.Created = profile.NewID(), s.now()
	}
	for i, o := range s.prof.list {
		if i != idx && o.Agent == p.Agent && o.Name == p.Name {
			http.Error(w, "another profile already has that name", http.StatusConflict)
			return
		}
	}
	if err := s.prof.secrets.Set(p.ID, sec); err != nil {
		s.log.Printf("save secrets: %v", err)
		http.Error(w, "couldn't save the key", http.StatusInternalServerError)
		return
	}
	old := append([]profile.Profile{}, s.prof.list...)
	if idx >= 0 {
		s.prof.list[idx] = p
	} else {
		s.prof.list = append(s.prof.list, p)
	}
	if err := s.saveProfilesLocked(); err != nil {
		s.prof.list = old
		s.log.Printf("save profiles: %v", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	s.gw.forget(p.ID)
	s.dirty.Store(true)
	if idx < 0 {
		w.WriteHeader(http.StatusCreated)
	}
	writeJSON(w, p)
}

// deleteProfile: DELETE /v1/profiles/{id}. Sessions keep the id they ran with, so their
// spend still shows (as a deleted profile).
func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.prof.mu.Lock()
	i, ok := s.findLocked(id)
	if !ok {
		s.prof.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	old := append([]profile.Profile{}, s.prof.list...)
	s.prof.list = append(s.prof.list[:i], s.prof.list[i+1:]...)
	if err := s.saveProfilesLocked(); err != nil {
		s.prof.list = old
		s.prof.mu.Unlock()
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	err := s.prof.secrets.Set(id, nil)
	s.prof.mu.Unlock()
	if err != nil {
		s.log.Printf("delete secrets: %v", err)
	}
	s.gw.forget(id)
	s.dirty.Store(true)
	w.WriteHeader(http.StatusNoContent)
}
