// Package codex translates documented Codex hooks into TRK's stored event vocabulary.
// It never reads transcripts or credentials and never controls the originating tool.
package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/GujaLomsadze/trk/internal/model"
)

// Events deliberately excludes Claude-only Notification/PostToolUseFailure.
var Events = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "Stop", "Interrupt", "SessionEnd", "PreCompact", "PostCompact", "SubagentStart", "SubagentStop"}

var exitLine = regexp.MustCompile(`(?m)^(?:Process exited with code|Exit code:)\s*(-?\d+)\s*$`)

// Normalize keeps the existing payload vocabulary so live state and history agree.
// The returned payload is normalized before storage; replay does not rerun this adapter.
func Normalize(raw json.RawMessage) (payload json.RawMessage, kind, native, key string, ok bool) {
	var p map[string]json.RawMessage
	if json.Unmarshal(raw, &p) != nil || p == nil {
		return
	}
	str := func(k string) string { var s string; _ = json.Unmarshal(p[k], &s); return s }
	native = str("session_id")
	if native == "" {
		return
	}
	event := str("hook_event_name")
	switch event {
	case "Interrupt":
		kind = model.KindInterrupt
	case "SubagentStart":
		kind = model.KindSubagentStart
	case "PostCompact":
		kind = model.KindCompacted
	default:
		kind = model.KindForHook(event)
	}
	set := func(k string, v any) { p[k], _ = json.Marshal(v) }
	set("trk_schema", 1)
	if id := str("tool_use_id"); id != "" {
		// Tool IDs are scoped to a turn. Preserve distinct repeated commands.
		if turn := str("turn_id"); turn != "" {
			set("tool_use_id", turn+"/"+id)
			if event == "PreToolUse" || event == "PostToolUse" {
				sum := sha256.Sum256([]byte(native + "\x00" + turn + "\x00" + id + "\x00" + event))
				key = "codex:" + hex.EncodeToString(sum[:])
			}
		}
	}
	if str("tool_name") == "apply_patch" {
		var in struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(p["tool_input"], &in)
		set("tool_files", PatchFiles(in.Command))
	}
	if event == "PostToolUse" {
		outcome := Outcome(p["tool_response"], str("tool_name"))
		set("tool_outcome", outcome)
		if outcome == "failed" {
			kind = model.KindToolFail
		}
	}
	// Quotas/status are not a documented hook field: never let these overwrite Claude's account.
	delete(p, "rate_limits")
	delete(p, "cost")
	delete(p, "context_window")
	payload, err := json.Marshal(p)
	ok = err == nil
	return
}

// Outcome accepts structured exit/error fields or the shell transport's exit trailer.
// Unfamiliar output is unknown, never presumed success or parsed for arbitrary error words.
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
		// Only read the transport header, never strings printed by the command itself.
		for _, marker := range []string{"Final output:", "Output:"} {
			if i := strings.Index(s, marker); i >= 0 {
				s = s[:i]
			}
		}
		matches := exitLine.FindAllStringSubmatch(s, -1)
		if len(matches) > 0 {
			n, err := strconv.Atoi(matches[len(matches)-1][1])
			if err == nil {
				if n != 0 {
					return "failed"
				}
				return "ok"
			}
		}
	}
	return "unknown"
}

// PatchFiles reads patch headers only, including both sides of moves. It never applies a patch.
func PatchFiles(patch string) []string {
	var files []string
	seen := map[string]bool{}
	for _, line := range strings.Split(patch, "\n") {
		for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if file, ok := strings.CutPrefix(line, prefix); ok {
				file = strings.TrimSpace(file)
				if file != "" && !seen[file] && len(files) < 200 {
					files = append(files, file)
					seen[file] = true
				}
			}
		}
	}
	return files
}
