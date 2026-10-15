package claudecfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

var (
	// Helm's hooks and status line, and TRK's (Helm's old name): init rewrites those to hlm.
	hlmHookRe   = regexp.MustCompile(`(?:^|[\s/\\"'])(?:hlm|trk)(?:\.exe)?["']?\s+hook(?:\s|$)`)
	hlmStatusRe = regexp.MustCompile(`(?:^|[\s/\\"'])(?:hlm|trk)(?:\.exe)?["']?\s+statusline(?:\s|$)`)
	// the program part of a status-line command that runs TRK or Helm, up to "statusline"
	ownStatusRe = regexp.MustCompile(`^\s*(?:'[^']*'|"[^"]*"|\S+)\s+statusline`)
)

// HookEvents are the Claude Code hook events hlm init registers.
var HookEvents = []string{
	"SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"PostToolUseFailure", "PermissionRequest", "Notification", "Stop", "SubagentStop", "PreCompact",
}

func MergeHooks(root *Object, hlmCmd string) (bool, error) {
	var hooks *Object
	switch v, ok := root.Get("hooks"); {
	case !ok || v == nil:
		hooks = NewObject()
		root.Set("hooks", hooks)
	default:
		h, isObj := v.(*Object)
		if !isObj {
			return false, errors.New(`"hooks" in settings.json is not an object; refusing to edit`)
		}
		hooks = h
	}
	want := hlmCmd + " hook"
	changed := false
	for _, ev := range HookEvents {
		var groups []any
		if v, ok := hooks.Get(ev); ok && v != nil {
			arr, isArr := v.([]any)
			if !isArr {
				return false, fmt.Errorf("hooks.%s in settings.json is not an array; refusing to edit", ev)
			}
			groups = arr
		}
		found, updated := updateHlmHook(groups, want)
		changed = changed || updated
		if found {
			continue
		}
		h := NewObject()
		h.Set("type", "command")
		h.Set("command", want)
		h.Set("async", true)
		h.Set("timeout", json.Number("10"))
		g := NewObject()
		g.Set("hooks", []any{h})
		hooks.Set(ev, append(groups, g))
		changed = true
	}
	return changed, nil
}

func updateHlmHook(groups []any, want string) (found, updated bool) {
	for _, g := range groups {
		gObj, ok := g.(*Object)
		if !ok {
			continue
		}
		hv, _ := gObj.Get("hooks")
		hs, _ := hv.([]any)
		for _, h := range hs {
			hObj, ok := h.(*Object)
			if !ok {
				continue
			}
			cv, _ := hObj.Get("command")
			cmd, _ := cv.(string)
			if !hlmHookRe.MatchString(cmd) {
				continue
			}
			found = true
			if cmd != want {
				hObj.Set("command", want)
				updated = true
			}
		}
	}
	return found, updated
}

type StatusAction string

const (
	StatusAdded   StatusAction = "added"
	StatusPresent StatusAction = "present"
	StatusChained StatusAction = "chained"
	StatusKept    StatusAction = "kept"
)

func ExistingStatusCommand(root *Object) string {
	v, _ := root.Get("statusLine")
	sl, ok := v.(*Object)
	if !ok {
		return ""
	}
	cv, _ := sl.Get("command")
	cmd, _ := cv.(string)
	return cmd
}

func MergeStatusLine(root *Object, hlmCmd string, chain bool) (StatusAction, error) {
	v, ok := root.Get("statusLine")
	if !ok || v == nil {
		sl := NewObject()
		sl.Set("type", "command")
		sl.Set("command", hlmCmd+" statusline")
		root.Set("statusLine", sl)
		return StatusAdded, nil
	}
	sl, isObj := v.(*Object)
	if !isObj {
		return StatusKept, nil
	}
	cmd := ExistingStatusCommand(root)
	if hlmStatusRe.MatchString(cmd) {
		// Ours already. Point it at this binary (TRK's old path, a moved install), keeping
		// whatever it chains after "statusline".
		if loc := ownStatusRe.FindStringIndex(cmd); loc != nil {
			if want := hlmCmd + " statusline" + cmd[loc[1]:]; want != cmd {
				sl.Set("command", want)
				return StatusAdded, nil
			}
		}
		return StatusPresent, nil
	}
	if cmd == "" || !chain {
		return StatusKept, nil
	}
	sl.Set("command", hlmCmd+" statusline --then "+ShellQuote(cmd))
	return StatusChained, nil
}
