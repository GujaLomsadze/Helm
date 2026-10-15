// Package claude is the Claude Code adapter. Claude's hook payloads are the model TRK's
// canonical schema grew from, so normalizing them only maps the event name to a kind.
package claude

import (
	"encoding/json"
	"strings"

	"github.com/GujaLomsadze/trk/internal/agent"
	"github.com/GujaLomsadze/trk/internal/claudecfg"
	"github.com/GujaLomsadze/trk/internal/model"
)

func init() { agent.Register(Adapter{}) }

type Adapter struct{}

func (Adapter) Name() string  { return "claude" }
func (Adapter) Label() string { return "Claude Code" }

func (Adapter) Caps() agent.Caps {
	// Background tasks can outlive a turn, so Stop doesn't close open tool calls.
	return agent.Caps{PIDAttribution: true, CwdBestMatch: true, Counters: true}
}

var hookKinds = map[string]string{
	"SessionStart":       model.KindSessionStart,
	"SessionEnd":         model.KindSessionEnd,
	"UserPromptSubmit":   model.KindPrompt,
	"PreToolUse":         model.KindToolPre,
	"PostToolUse":        model.KindToolPost,
	"PostToolUseFailure": model.KindToolFail,
	"PermissionRequest":  model.KindPermission,
	"Notification":       model.KindNotification,
	"Stop":               model.KindStop,
	"SubagentStop":       model.KindSubagentStop,
	"PreCompact":         model.KindCompact,
}

// KindForHook maps a Claude Code hook event to an event kind.
func KindForHook(event string) string {
	if k, ok := hookKinds[event]; ok {
		return k
	}
	return model.KindHookOther
}

func (Adapter) Normalize(raw json.RawMessage) (agent.Normalized, bool) {
	var p struct {
		SessionID string `json:"session_id"`
		Event     string `json:"hook_event_name"`
	}
	_ = json.Unmarshal(raw, &p) // a payload without these still counts; attribution falls back
	return agent.Normalized{Payload: raw, Kind: KindForHook(p.Event), Native: p.SessionID}, true
}

func (Adapter) HookReply(json.RawMessage) string          { return "" }
func (Adapter) SessionFromEnv(func(string) string) string { return "" } // found by process tree instead

// Launch: `claude [--resume id [--fork-session]] [prompt]`.
func (Adapter) Launch(sp agent.LaunchSpec) (agent.Launch, error) {
	var a []string
	if sp.Resume != "" {
		a = append(a, "--resume", sp.Resume)
		if sp.Fork {
			a = append(a, "--fork-session")
		}
	}
	if p := strings.TrimSpace(sp.Prompt); p != "" {
		a = append(a, p)
	}
	return agent.Launch{Exe: "claude", Args: a}, nil
}

// PrivateEnv: the daemon is spawned by Claude Code hooks, so it carries CLAUDECODE,
// CLAUDE_CODE_CHILD_SESSION, the session id and its messaging socket/token; a child
// claude that inherits them runs as that session's sub-agent and saves no transcript.
func (Adapter) PrivateEnv(k string) bool {
	return k == "CLAUDECODE" || k == "CLAUDE_PID" || k == "CLAUDE_EFFORT" || strings.HasPrefix(k, "CLAUDE_CODE_")
}

func (Adapter) ResumeCommand(native string) string { return "claude --resume " + native }

func (Adapter) Install(o agent.InstallOptions) (agent.InstallResult, error) {
	paths, err := claudecfg.DefaultPaths()
	if err != nil {
		return agent.InstallResult{}, err
	}
	if o.Instructions != "" {
		paths.ClaudeMD = o.Instructions
	}
	var chain func(string) bool
	if o.Confirm != nil {
		chain = func(existing string) bool {
			return o.Confirm("You already have a status line: " + existing + "\n    Chain it through trk? It keeps showing exactly as before.")
		}
	}
	r, err := claudecfg.Init(claudecfg.InitOptions{Paths: paths, TrkCmd: o.TrkCmd, ChainStatus: chain,
		SkipStatusLine: o.SkipStatusLine, SkipClaudeMD: o.SkipInstructions, DryRun: o.DryRun, Now: o.Now})
	if err != nil {
		return agent.InstallResult{}, err
	}
	var res agent.InstallResult
	step := func(label string, changed bool, detail, backup string) {
		st := agent.InstallStep{Label: label, State: agent.StepPresent, Detail: "already set up"}
		if changed {
			st = agent.InstallStep{Label: label, State: agent.StepAdded, Detail: detail, Backup: backup}
		}
		res.Steps = append(res.Steps, st)
	}
	step("Hooks", r.HooksChanged, paths.Settings, r.SettingsBackup)
	switch r.Status {
	case claudecfg.StatusAdded:
		res.Steps = append(res.Steps, agent.InstallStep{Label: "Status line", State: agent.StepAdded, Detail: "set to trk", Backup: r.SettingsBackup})
	case claudecfg.StatusChained:
		res.Steps = append(res.Steps, agent.InstallStep{Label: "Status line", State: agent.StepAdded, Detail: "chained — " + r.ExistingStatus + " still shows, unchanged", Backup: r.SettingsBackup})
	case claudecfg.StatusPresent:
		res.Steps = append(res.Steps, agent.InstallStep{Label: "Status line", State: agent.StepPresent, Detail: "already goes through trk"})
	case claudecfg.StatusKept:
		res.Steps = append(res.Steps, agent.InstallStep{Label: "Status line", State: agent.StepKept, Detail: "kept " + r.ExistingStatus + " — re-run with --yes to chain it"})
	}
	if !o.SkipInstructions {
		step("CLAUDE.md", r.ClaudeMDChanged, paths.ClaudeMD, r.ClaudeMDBackup)
	}
	res.Next = []string{"Restart your Claude Code sessions (hooks load at session start)"}
	return res, nil
}
