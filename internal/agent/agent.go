// Package agent is the boundary between Helm's core and the coding agents it watches.
// Each agent (Claude Code, Codex, …) is one Adapter in its own package. The core —
// fleet, derive, store, stats, daemon — reads only Helm's canonical event schema (see
// model) and an adapter's Caps; it never compares agent names. Adding an agent means
// one new package and one line in agent/all.
package agent

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// Default is the agent an unlabelled hook or terminal belongs to, for compatibility
// with installs from before Helm knew more than one agent.
const Default = "claude"

// Caps are the behaviours the core switches on. They describe how an agent reports,
// not who it is.
type Caps struct {
	// PIDAttribution: one OS process per session, so Helm learns pid → session from hooks
	// and can tell whether a session is still running (Claude Code).
	PIDAttribution bool
	// CwdBestMatch: progress reports with no session id go to the best match in the
	// folder. Without it only a single live match in the folder is used.
	CwdBestMatch bool
	// StopEndsTools: a turn ending closes tool calls that never reported back.
	StopEndsTools bool
	// Counters: the agent reports cumulative cost, tokens and model time.
	Counters bool
}

// Normalized is one hook delivery in Helm's canonical schema.
type Normalized struct {
	Payload    json.RawMessage
	Kind       string // model.Kind*
	Native     string // the agent's own session id
	Key        string // stable delivery id for dedupe; "" when the agent gives none
	Background bool   // the agent's own internal work, not a session the user started
	Defer      bool   // keep the card hidden until the session does something
}

// Launch is what a dashboard terminal runs. Args never pass through a shell.
type Launch struct {
	Exe  string
	Args []string
}

// LaunchSpec is a dashboard request to start, resume or fork a session.
type LaunchSpec struct {
	Prompt string
	Resume string // native session id
	Fork   bool
}

// InstallOptions are the choices `hlm init --agent X` passes on.
type InstallOptions struct {
	HlmCmd           string // how hooks should call hlm: absolute, quoted when needed
	DryRun           bool
	Instructions     string // instruction file to add the progress block to ("" = the agent's default)
	SkipInstructions bool
	SkipStatusLine   bool
	Confirm          func(question string) bool // nil = no: never prompt
	Now              time.Time
}

// InstallStep is one line of `hlm init` output.
type InstallStep struct {
	Label  string // "Hooks", "Status line", "CLAUDE.md"
	State  string // StepAdded, StepPresent, …
	Detail string // path or note
	Backup string // where the old file went, when it was changed
}

const (
	StepAdded   = "added"   // changed (or would change, in a dry run)
	StepPresent = "present" // already set up
	StepKept    = "kept"    // left alone on purpose; Detail says why
)

type InstallResult struct {
	Steps []InstallStep
	Next  []string // what the user does after init, e.g. restart the agent
}

// Changed reports whether any step changed (or would change) a file.
func (r InstallResult) Changed() bool {
	for _, s := range r.Steps {
		if s.State == StepAdded {
			return true
		}
	}
	return false
}

// Adapter translates one agent's hooks, installs its integration and launches it.
type Adapter interface {
	Name() string  // "claude": stored on events and sessions, used in URLs and flags
	Label() string // "Claude Code": shown to people
	Caps() Caps
	// Normalize turns a raw hook payload into canonical form. ok=false drops it.
	Normalize(raw json.RawMessage) (n Normalized, ok bool)
	// HookReply is what `hlm hook --agent X` prints for this payload ("" = nothing).
	HookReply(raw json.RawMessage) string
	// SessionFromEnv finds the session a `hlm step` call runs in, from the agent's
	// environment variables. "" when the agent doesn't provide one.
	SessionFromEnv(getenv func(string) string) string
	// Launch builds the argv for a dashboard terminal.
	Launch(sp LaunchSpec) (Launch, error)
	// PrivateEnv reports whether an inherited variable belongs to an agent session and
	// must not leak into a terminal Helm starts (it would join that session).
	PrivateEnv(key string) bool
	// ResumeCommand is the shell command that continues a session outside Helm.
	ResumeCommand(native string) string
	Install(o InstallOptions) (InstallResult, error)
}

// LimitWindow is one plan-usage window, e.g. a 5-hour or weekly quota.
type LimitWindow struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	DurationMin int64    `json:"duration_min,omitempty"`
	UsedPct     *float64 `json:"used_pct"`
	ResetAt     int64    `json:"reset_at,omitempty"` // unix ms
}

// LimitsReader is optional: an adapter that can read the account's plan usage on demand.
type LimitsReader interface {
	Limits(ctx context.Context) ([]LimitWindow, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Adapter{}
)

// Register adds an adapter; agent packages call it from init.
func Register(a Adapter) {
	mu.Lock()
	defer mu.Unlock()
	registry[a.Name()] = a
}

func Get(name string) (Adapter, bool) {
	mu.RLock()
	defer mu.RUnlock()
	a, ok := registry[name]
	return a, ok
}

// All returns every adapter, the default first, then by name.
func All() []Adapter {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Adapter, 0, len(registry))
	for _, a := range registry {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Name() == Default) != (out[j].Name() == Default) {
			return out[i].Name() == Default
		}
		return out[i].Name() < out[j].Name()
	})
	return out
}

// CapsOf is the adapter's caps, or none for an unknown agent.
func CapsOf(name string) Caps {
	if a, ok := Get(name); ok {
		return a.Caps()
	}
	return Caps{}
}

// SessionKey is Helm's session id for an agent's native one. The default agent keeps
// its ids unchanged (they predate this scheme); the others are "<agent>:<native>", so
// two agents can never share a key.
func SessionKey(agentName, native string) string {
	if native == "" || agentName == "" || agentName == Default || strings.HasPrefix(native, agentName+":") {
		return native
	}
	return agentName + ":" + native
}

// Split reverses SessionKey: the agent a key belongs to (by prefix; "" when it has
// none) and its native id.
func Split(key string) (agentName, native string) {
	if name, rest, ok := strings.Cut(key, ":"); ok {
		if _, known := Get(name); known && name != Default {
			return name, rest
		}
	}
	return "", key
}
