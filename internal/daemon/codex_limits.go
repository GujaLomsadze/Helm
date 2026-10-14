package daemon

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/GujaLomsadze/trk/internal/codex"
)

type codexLimitsView struct {
	Buckets   []codex.LimitBucket `json:"buckets"`
	UpdatedAt int64               `json:"updated_at"`
	Error     string              `json:"error,omitempty"`
	Stale     bool                `json:"stale"`
}

type codexLimitsCache struct {
	mu   sync.Mutex
	at   int64
	view codexLimitsView
	read func(context.Context) ([]codex.LimitBucket, error)
}

// Requests share one bounded read per minute, retrying failures after 15s. Nothing polls
// when the Limits page is closed, and Claude's account/history remain separate.
func (s *Server) getCodexLimits(w http.ResponseWriter, r *http.Request) {
	c := &s.codexLimits
	c.mu.Lock()
	defer c.mu.Unlock()
	cacheMs := int64(60000)
	if c.view.Stale {
		cacheMs = 15000
	}
	if c.at == 0 || s.now()-c.at >= cacheMs {
		read := c.read
		if read == nil {
			read = codex.ReadLimits
		}
		// This result is shared by all viewers. A tab closing or reloading must
		// not cancel the refresh and cache an error for everyone else.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 12*time.Second)
		buckets, err := read(ctx)
		cancel()
		c.at = s.now()
		if err != nil {
			c.view.Error = err.Error()
			c.view.Stale = true
		} else {
			c.view = codexLimitsView{Buckets: buckets, UpdatedAt: c.at}
		}
	}
	writeJSON(w, c.view)
}
