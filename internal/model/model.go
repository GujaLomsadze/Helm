// Package model defines the wire envelope, stored events and materialized state.
//
// Canonical payload: every adapter turns its agent's hook JSON into these fields (the
// names follow Claude Code's hooks, where Helm started). The core reads nothing else.
//
//	session_id, cwd, model             the session, its folder, its model (string or {display_name,id})
//	prompt                             user prompt (prompt kind)
//	tool_name, tool_input, tool_use_id one tool call; tool_input.command / .file_path are read when present
//	tool_files   []string              files a call touched, when the tool names more than one (patches)
//	tool_kind    "read"|"edit"|"run"   what a call does, when tool_name alone doesn't say
//	tool_outcome "ok"|"failed"|"unknown" how a call ended, when the agent reports it
//	message, notification_type         notifications (permission_prompt, elicitation_dialog)
//	last_assistant_message             the agent's last reply (stop kind)
//	agent_id, agent_type               subagents
//	background_tasks [{status}]        tasks still running when a turn ends
//	cost, context_window, rate_limits  status-line counters (agents with Caps.Counters)
//	hlm_background, hlm_defer          set by Helm from the adapter (see agent.Normalized)
package model

import "encoding/json"

// Envelope is what clients POST to /v1/events.
type Envelope struct {
	Agent      string          `json:"agent,omitempty"`       // adapter that sent a hook ("" = claude, from older installs)
	TerminalID string          `json:"terminal_id,omitempty"` // Helm terminal the agent runs in (HLM_TERMINAL_ID)
	TS         int64           `json:"ts,omitempty"`          // unix ms at the caller
	Source     string          `json:"source"`                // cli|hook|statusline|api
	Kind       string          `json:"kind,omitempty"`        // empty for hook/statusline: derived by the daemon
	SessionID  string          `json:"session_id,omitempty"`  // explicit session (HLM_SESSION or third-party)
	SessionVia string          `json:"session_via,omitempty"` // "env" when SessionID came from HLM_SESSION
	ClaudePID  int             `json:"claude_pid,omitempty"`  // nearest ancestor agent process with PID attribution (claude), 0 if none
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type Event struct {
	Agent       string          `json:"agent,omitempty"`     // set by native hook/status events only
	NativeID    string          `json:"native_id,omitempty"` // the agent's own session id
	Key         string          `json:"-"`                   // adapter dedupe key; "" = none
	ID          int64           `json:"id"`
	TS          int64           `json:"ts"`
	SessionID   string          `json:"session_id"`
	Source      string          `json:"source"`
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload"`
	Attribution string          `json:"attribution"`
}

type Session struct {
	SessionID   string   `json:"session_id"`
	Agent       string   `json:"agent"`               // learned from native events; "" = never seen one
	NativeID    string   `json:"native_id,omitempty"` // the agent's own id (for resume commands)
	Background  bool     `json:"-"`                   // the agent's internal work: never shown or counted
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"` // chosen in the dashboard; shown instead of Name
	Cwd         string   `json:"cwd"`
	Repo        string   `json:"repo"`
	Branch      string   `json:"branch"`
	Model       string   `json:"model"`
	Task        string   `json:"task"`
	StepText    string   `json:"step_text"`
	StepI       int      `json:"step_i"`
	StepN       int      `json:"step_n"`
	Status      string   `json:"status"`
	CtxPct      *float64 `json:"ctx_pct"`
	CtxSize     int64    `json:"ctx_size"`
	CostUSD     float64  `json:"cost_usd"`
	TokensIn    int64    `json:"tokens_in"`
	TokensOut   int64    `json:"tokens_out"`
	StartedAt   int64    `json:"started_at"`
	LastEventAt int64    `json:"last_event_at"`
	DismissedAt int64    `json:"-"` // ✕ on the card; hidden until new activity after this time
	// Card widgets, from hooks and the status line.
	LastPrompt   string         `json:"last_prompt,omitempty"`
	LastPromptAt int64          `json:"last_prompt_at,omitempty"`
	LastReply    string         `json:"last_reply,omitempty"` // main agent's last message (Stop hook)
	LastReplyAt  int64          `json:"last_reply_at,omitempty"`
	LinesAdded   int64          `json:"lines_added"`
	LinesRemoved int64          `json:"lines_removed"`
	APIMs        int64          `json:"api_ms"`              // time the model was working
	WallMs       int64          `json:"wall_ms"`             // session wall time
	Subagents    map[string]int `json:"subagents,omitempty"` // finished subagents by type; replaced, never mutated
	BgRunning    int            `json:"bg_running"`          // background tasks still running at the last Stop
}

// Account is the latest plan-limit snapshot. Resets are unix ms.
type Account struct {
	FiveHPct    *float64 `json:"five_h_pct"`
	FiveHReset  int64    `json:"five_h_reset"`
	SevenDPct   *float64 `json:"seven_d_pct"`
	SevenDReset int64    `json:"seven_d_reset"`
	UpdatedAt   int64    `json:"updated_at"`
}

const (
	SourceCLI        = "cli"
	SourceHook       = "hook"
	SourceStatusline = "statusline"
	SourceAPI        = "api"
)

const (
	AttrEnv    = "env"
	AttrPtree  = "ptree"
	AttrCwd    = "cwd"
	AttrNative = "native"
)

const (
	KindStart         = "start"
	KindStep          = "step"
	KindProgress      = "progress"
	KindBlocked       = "blocked"
	KindDone          = "done"
	KindSessionStart  = "session_start"
	KindSessionEnd    = "session_end"
	KindPrompt        = "prompt"
	KindToolPre       = "tool_pre"
	KindToolPost      = "tool_post"
	KindToolFail      = "tool_fail"
	KindPermission    = "permission"
	KindNotification  = "notification"
	KindStop          = "stop"
	KindSubagentStop  = "subagent_stop"
	KindCompact       = "compact"
	KindCompacted     = "compacted" // compaction finished
	KindInterrupt     = "interrupt" // the user stopped the turn
	KindSubagentStart = "subagent_start"
	KindStatus        = "status"
	KindHookOther     = "hook"
	KindOther         = "event"
)

// IsActivity reports whether an event kind means the agent is doing something
// (as opposed to bookkeeping: status updates, notifications, turn ends).
// SubagentStop is not activity: Claude Code fires one for an internal agent right
// after every Stop, and a real subagent's work already shows up as tool events.
func IsActivity(kind string) bool {
	switch kind {
	case KindStart, KindStep, KindProgress, KindBlocked, KindDone, KindSessionStart, KindPrompt,
		KindToolPre, KindToolPost, KindToolFail, KindPermission, KindCompact:
		return true
	}
	return false
}
