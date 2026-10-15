// Package codex is the OpenAI Codex adapter. It translates Codex's lifecycle hooks into
// Helm's canonical schema, installs those hooks, launches `codex` in dashboard terminals
// and reads the account's plan usage. It never reads transcripts or credentials and never
// steers Codex: hooks get no decision back.
package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/GujaLomsadze/helm/internal/agent"
	"github.com/GujaLomsadze/helm/internal/model"
)

// Events are the Codex hook events `hlm init --agent codex` registers.
var Events = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "Stop", "Interrupt", "SessionEnd", "PreCompact", "PostCompact", "SubagentStart", "SubagentStop"}

var hookKinds = map[string]string{
	"SessionStart":      model.KindSessionStart,
	"SessionEnd":        model.KindSessionEnd,
	"UserPromptSubmit":  model.KindPrompt,
	"PreToolUse":        model.KindToolPre,
	"PostToolUse":       model.KindToolPost,
	"PermissionRequest": model.KindPermission,
	"Stop":              model.KindStop,
	"Interrupt":         model.KindInterrupt,
	"PreCompact":        model.KindCompact,
	"PostCompact":       model.KindCompacted,
	"SubagentStart":     model.KindSubagentStart,
	"SubagentStop":      model.KindSubagentStop,
}

// suggestionPreamble opens the desktop app's hidden "suggest the next message" requests.
// They run as separate forks with their own ids, and hooks carry no field saying so; the
// full opening sentences are matched, never generic words.
const suggestionPreamble = "This is an app-generated request for a suggested next user message. The user did not write this message."

var exitLine = regexp.MustCompile(`(?m)^(?:Process exited with code|Exit code:)\s*(-?\d+)\s*$`)

// Normalize maps one hook delivery onto the canonical schema. The result is what gets
// stored, so a replay after restart reads exactly what the live path read.
func (Adapter) Normalize(raw json.RawMessage) (agent.Normalized, bool) {
	var n agent.Normalized
	var p map[string]json.RawMessage
	if json.Unmarshal(raw, &p) != nil || p == nil {
		return n, false
	}
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(p[k], &s)
		return s
	}
	if n.Native = str("session_id"); n.Native == "" {
		return n, false
	}
	event, tool := str("hook_event_name"), str("tool_name")
	n.Kind = model.KindHookOther
	if k, ok := hookKinds[event]; ok {
		n.Kind = k
	}
	set := func(k string, v any) { p[k], _ = json.Marshal(v) }
	if id := str("tool_use_id"); id != "" {
		// Tool ids repeat across turns: scope them, or a later turn's call would close an earlier one.
		if turn := str("turn_id"); turn != "" {
			set("tool_use_id", turn+"/"+id)
			if n.Kind == model.KindToolPre || n.Kind == model.KindToolPost {
				sum := sha256.Sum256([]byte(n.Native + "\x00" + turn + "\x00" + id + "\x00" + event))
				n.Key = "codex:" + hex.EncodeToString(sum[:])
			}
		}
	}
	if tool == "apply_patch" {
		var in struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(p["tool_input"], &in)
		set("tool_files", PatchFiles(in.Command))
		set("tool_kind", "edit")
	}
	if n.Kind == model.KindToolPost {
		outcome := Outcome(p["tool_response"], tool)
		set("tool_outcome", outcome)
		if outcome == "failed" {
			n.Kind = model.KindToolFail
		}
	}
	switch n.Kind {
	case model.KindPrompt:
		n.Background = strings.HasPrefix(strings.TrimSpace(str("prompt")), suggestionPreamble)
	case model.KindSessionStart:
		n.Defer = str("source") == "fork" // shown once it does something; suggestion forks never do visibly
	}
	// Codex hooks carry no account or usage data; never let a stray field look like Claude's.
	delete(p, "rate_limits")
	delete(p, "cost")
	delete(p, "context_window")
	b, err := json.Marshal(p)
	if err != nil {
		return n, false
	}
	n.Payload = b
	return n, true
}

// HookReply: Stop handlers must answer with JSON; an empty object steers nothing.
func (Adapter) HookReply(raw json.RawMessage) string {
	var p struct {
		Event string `json:"hook_event_name"`
	}
	_ = json.Unmarshal(raw, &p)
	if p.Event == "Stop" || p.Event == "SubagentStop" {
		return "{}"
	}
	return ""
}

// Outcome reads structured exit/error fields, or the shell transport's exit trailer.
// Anything else is unknown: output is never searched for error words.
func Outcome(raw json.RawMessage, tool string) string {
	var o struct {
		ExitCode *int  `json:"exit_code"`
		IsError  *bool `json:"isError"`
	}
	if json.Unmarshal(raw, &o) == nil {
		if o.IsError != nil {
			if *o.IsError {
				return "failed"
			}
			return "ok"
		}
		if o.ExitCode != nil {
			if *o.ExitCode != 0 {
				return "failed"
			}
			return "ok"
		}
	}
	var s string
	if tool == "Bash" && json.Unmarshal(raw, &s) == nil {
		// Only the transport header counts, never what the command printed.
		for _, marker := range []string{"Final output:", "Output:"} {
			if i := strings.Index(s, marker); i >= 0 {
				s = s[:i]
			}
		}
		if m := exitLine.FindAllStringSubmatch(s, -1); len(m) > 0 {
			if n, err := strconv.Atoi(m[len(m)-1][1]); err == nil {
				if n != 0 {
					return "failed"
				}
				return "ok"
			}
		}
	}
	return "unknown"
}

// PatchFiles lists the files an apply_patch touches, from its headers only (both sides of
// a move), at most 200.
func PatchFiles(patch string) []string {
	files := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(patch, "\n") {
		for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if file, ok := strings.CutPrefix(line, prefix); ok {
				if file = strings.TrimSpace(file); file != "" && !seen[file] && len(files) < 200 {
					files = append(files, file)
					seen[file] = true
				}
			}
		}
	}
	return files
}
