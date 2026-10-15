package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/GujaLomsadze/helm/internal/agent"
	"github.com/GujaLomsadze/helm/internal/derive"
	"github.com/GujaLomsadze/helm/internal/fleet"
	"github.com/GujaLomsadze/helm/internal/ptree"
	"github.com/GujaLomsadze/helm/internal/term"
	"github.com/gorilla/websocket"
)

// Agents started from the dashboard, each running in a pty owned by the daemon. Off
// unless the user turns it on in the Card layout drawer (stored server-side, so a page
// can't enable it for itself). The same Host/Origin guard as every route applies.

// settingTerminals keeps the key from when terminals were experimental, so the choice survives.
const settingTerminals = "experimental_terminals"

// dashView is the dashboard snapshot: the fleet plus the terminal state.
type dashView struct {
	fleet.View
	TerminalsEnabled   bool        `json:"terminals_enabled"`
	TerminalsSupported bool        `json:"terminals_supported"`
	Terminals          []term.Info `json:"terminals"`
	// Running holds every session known to be alive, mapped to the Helm terminal it runs
	// in, or "" when it runs somewhere else (a terminal tab, an IDE). Sessions in live Helm
	// terminals always count; elsewhere only agents with PID attribution can be checked.
	Running map[string]string `json:"running"`
}

func (s *Server) dash() dashView {
	v := s.fl.View(s.now())
	shown := make(map[string]bool, len(v.Sessions))
	for _, sv := range v.Sessions {
		shown[sv.SessionID] = true
	}
	running := s.linkSessions(shown) // before List, so the terminals carry their session
	return dashView{View: v, TerminalsEnabled: s.termsOn.Load(),
		TerminalsSupported: term.Supported, Terminals: s.terms.List(), Running: running}
}

// linkSessions finds the sessions still running and links each Helm terminal to the
// session inside it: by HLM_TERMINAL_ID (postEvent) or, for agents with PID attribution,
// by an agent process at or below the terminal's pid.
// only, when not nil, limits the check to those sessions: the pid map keeps every
// session ever seen, and the snapshot is rebuilt several times a second.
func (s *Server) linkSessions(only map[string]bool) map[string]string {
	out := map[string]string{}
	var live []term.Info
	for _, t := range s.terms.List() {
		if !t.Exited {
			live = append(live, t)
			if t.SessionID != "" && (only == nil || only[t.SessionID]) {
				out[t.SessionID] = t.ID
			}
		}
	}
	pids := s.fl.ClaudePIDs()
	if len(pids) == 0 {
		return out
	}
	tb := ptree.Snapshot()
	for pid, sid := range pids {
		if only != nil && !only[sid] {
			continue
		}
		if !tb.IsClaude(pid) { // ptree knows Claude Code's process; PID attribution is Claude's today
			continue // exited, or the pid now belongs to something else
		}
		owner := ""
		for _, t := range live {
			if tb.Descends(pid, t.PID) {
				owner = t.ID
				if s.terms.Link(t.ID, sid) && t.Title != "" {
					s.titleOnce(sid, t.Title) // the name typed in "+ New agent"
				}
				break
			}
		}
		if prev, seen := out[sid]; !seen || prev == "" {
			out[sid] = owner
		}
	}
	return out
}

// linkTerminal ties a terminal to the first session that reports from it.
func (s *Server) linkTerminal(termID, sid string) {
	if !s.terms.LinkFirst(termID, sid) {
		return
	}
	if t, ok := s.terms.Get(termID); ok && t.Info().Title != "" {
		s.titleOnce(sid, t.Info().Title) // the name typed in "+ New agent"
	}
	s.dirty.Store(true)
}

// titleOnce names a newly linked session unless it already has a title.
func (s *Server) titleOnce(sid, title string) {
	if row, ok := s.fl.SessionRow(sid); ok && row.Title == "" {
		s.applyTitle(sid, title)
	}
}

func (s *Server) applyTitle(sid, title string) {
	if s.fl.SetTitle(sid, title) != nil {
		return
	}
	if row, ok := s.fl.SessionRow(sid); ok {
		if err := s.st.UpsertSession(row); err != nil {
			s.log.Printf("persist title: %v", err)
		}
	}
	s.dirty.Store(true)
}

// SetTerminals turns the terminals on or off (Run restores it from the store).
func (s *Server) SetTerminals(on bool) { s.termsOn.Store(on); s.dirty.Store(true) }

// CloseTerminals kills every dashboard-started agent; Run calls it on shutdown.
func (s *Server) CloseTerminals() { s.terms.Close() }

func (s *Server) listTerms(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"enabled": s.termsOn.Load(), "running": s.terms.Running(), "terminals": s.terms.List()})
}

func (s *Server) startTerm(w http.ResponseWriter, r *http.Request) {
	if !s.termsOn.Load() {
		http.Error(w, "terminals are off (⚙ Card layout → Terminals)", http.StatusForbidden)
		return
	}
	var sp term.Spec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&sp); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	title, err := fleet.CleanTitle(sp.Title)
	if err != nil {
		http.Error(w, fmt.Sprintf("name: %v (at most %d characters)", err, fleet.MaxTitle), http.StatusBadRequest)
		return
	}
	sp.Title = title
	if sp.Agent == "" {
		sp.Agent = agent.Default
	}
	if _, ok := agent.Get(sp.Agent); !ok {
		http.Error(w, "unknown agent", http.StatusBadRequest)
		return
	}
	sid := agent.SessionKey(sp.Agent, sp.Resume)
	// Two agents on one conversation would both write to it.
	if sp.Resume != "" && !sp.Fork {
		if where, ok := s.linkSessions(map[string]bool{sid: true})[sid]; ok {
			msg := "that session is still running in another terminal: stop the agent there first, or fork it"
			if where != "" {
				msg = "that session is already running in a Helm terminal"
			}
			http.Error(w, msg, http.StatusConflict)
			return
		}
		// Without PID attribution Helm can't see a process; a busy card is the best evidence.
		if !agent.CapsOf(sp.Agent).PIDAttribution {
			if sv, ok := s.fl.SessionView(sid, s.now()); ok && !derive.Quiet(sv.Status) {
				http.Error(w, "that session looks active: stop it where it runs first, or fork it", http.StatusConflict)
				return
			}
		}
	}
	t, err := s.terms.Start(sp)
	if errors.Is(err, term.ErrUnsupported) {
		http.Error(w, err.Error(), http.StatusNotImplemented)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.log.Printf("terminal %s: %s %v in %s (pid %d)", t.Info().ID, sp.Agent, sp.Resume, t.Info().Dir, t.Info().PID)
	if sp.Resume != "" && !sp.Fork {
		s.terms.DropExited(sid, t.Info().ID) // the resumed terminal replaces the one that ended
		if sp.Title != "" {
			s.applyTitle(sid, sp.Title) // a resume names a session we already know
		}
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, t.Info())
}

func (s *Server) stopTerm(w http.ResponseWriter, r *http.Request) {
	if err := s.terms.Stop(r.PathValue("id")); err != nil {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// guard() has already rejected foreign Hosts and Origins, so the upgrader accepts the rest.
var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// attachTerm bridges one viewer to a terminal: the scrollback first, then live output as
// binary frames. From the viewer, binary frames are keystrokes and text frames are
// {"cols","rows"} resizes. A text frame {"exit":true} tells the viewer the process ended.
func (s *Server) attachTerm(w http.ResponseWriter, r *http.Request) {
	t, ok := s.terms.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	snap, out, detach := t.Attach()
	defer detach()
	if len(snap) > 0 && ws.WriteMessage(websocket.BinaryMessage, snap) != nil {
		return
	}
	go func() {
		for b := range out {
			if ws.WriteMessage(websocket.BinaryMessage, b) != nil {
				return
			}
		}
		if t.Info().Exited {
			_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"exit":true}`))
		}
		_ = ws.Close() // dropped for lagging: the viewer reconnects and replays the scrollback
	}()
	for {
		kind, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if kind == websocket.TextMessage {
			var sz struct{ Cols, Rows uint16 }
			if json.Unmarshal(data, &sz) == nil && sz.Cols > 0 && sz.Rows > 0 {
				_ = t.Resize(sz.Cols, sz.Rows)
			}
			continue
		}
		if t.Write(data) != nil {
			return
		}
	}
}
