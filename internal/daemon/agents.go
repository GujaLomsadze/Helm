package daemon

import (
	"context"
	"io/fs"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/GujaLomsadze/helm/internal/agent"
	"github.com/GujaLomsadze/helm/web"
)

// agentsSeenWindow: an agent counts as "in use" (and gets its UI) after a session in this long.
const agentsSeenWindow = 30 * 24 * 3600 * 1000

type agentInfo struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Seen      bool   `json:"seen"`      // had a session recently: the dashboard shows its controls
	Available bool   `json:"available"` // its CLI is on Helm's PATH: "+ Agent" can start it
	Limits    bool   `json:"limits"`    // /v1/agents/{name}/limits can read its plan usage
	Counters  bool   `json:"counters"`  // reports cost and tokens
	Resume    string `json:"resume"`    // resume command, with {id} for the native session id
	Icon      bool   `json:"icon"`      // web/icons/<name>.svg exists
}

// getAgents lists every adapter for the dashboard: which agents to show and what each can do.
func (s *Server) getAgents(w http.ResponseWriter, r *http.Request) {
	seen := map[string]bool{}
	if names, err := s.st.Agents(s.now() - agentsSeenWindow); err == nil {
		for _, n := range names {
			seen[n] = true
		}
	}
	out := []agentInfo{}
	for _, a := range agent.All() {
		_, limits := a.(agent.LimitsReader)
		l, err := a.Launch(agent.LaunchSpec{})
		available := s.terms.Command != "" // tests and demos run another program for every agent
		if !available && err == nil {
			_, err = exec.LookPath(l.Exe)
			available = err == nil
		}
		out = append(out, agentInfo{Name: a.Name(), Label: a.Label(), Seen: seen[a.Name()], Available: available,
			Limits: limits, Counters: a.Caps().Counters, Resume: a.ResumeCommand("{id}"), Icon: hasIcon(a.Name())})
	}
	writeJSON(w, out)
}

func hasIcon(name string) bool {
	_, err := fs.Stat(web.FS, "icons/"+name+".svg")
	return err == nil
}

const (
	limitsTTL      = 60 * 1000 // one read a minute, shared by every open Limits page
	limitsRetryTTL = 15 * 1000 // after a failed read
	limitsTimeout  = 12 * time.Second
)

type limitsView struct {
	Windows   []agent.LimitWindow `json:"windows"`
	UpdatedAt int64               `json:"updated_at"` // last successful read
	Error     string              `json:"error,omitempty"`
	Stale     bool                `json:"stale"` // the last read failed; Windows are older
}

type limitsEntry struct {
	at      int64 // last attempt
	view    limitsView
	reading chan struct{} // closed when the read in flight finishes
}

type limitsCache struct {
	mu sync.Mutex
	m  map[string]*limitsEntry
}

// getAgentLimits serves an agent's plan usage, read on demand. Nothing polls while no
// Limits page is open; concurrent viewers share one read, and the lock is not held while
// the agent is asked.
func (s *Server) getAgentLimits(w http.ResponseWriter, r *http.Request) {
	a, ok := agent.Get(r.PathValue("name"))
	reader, can := a.(agent.LimitsReader)
	if !ok || !can {
		http.NotFound(w, r)
		return
	}
	c := &s.limits
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]*limitsEntry{}
	}
	e := c.m[a.Name()]
	if e == nil {
		e = &limitsEntry{}
		c.m[a.Name()] = e
	}
	ttl := int64(limitsTTL)
	if e.view.Stale {
		ttl = limitsRetryTTL
	}
	if e.reading == nil && (e.at == 0 || s.now()-e.at >= ttl) {
		done := make(chan struct{})
		e.reading = done
		go func() {
			// Shared by every viewer: a tab closing must not cancel it for the rest.
			ctx, cancel := context.WithTimeout(context.Background(), limitsTimeout)
			wins, err := reader.Limits(ctx)
			cancel()
			c.mu.Lock()
			e.at, e.reading = s.now(), nil
			if err != nil {
				e.view.Error, e.view.Stale = err.Error(), true
			} else {
				e.view = limitsView{Windows: wins, UpdatedAt: e.at}
			}
			c.mu.Unlock()
			close(done)
		}()
	}
	wait := e.reading
	c.mu.Unlock()
	if wait != nil {
		select {
		case <-wait:
		case <-r.Context().Done():
			return
		}
	}
	c.mu.Lock()
	v := e.view
	c.mu.Unlock()
	if v.Windows == nil {
		v.Windows = []agent.LimitWindow{}
	}
	writeJSON(w, v)
}
