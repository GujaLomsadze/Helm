package claudecfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

var (
	trkHookRe   = regexp.MustCompile(`(?:^|[\s/\\"'])trk(?:\.exe)?["']?\s+hook(?:\s|$)`)
	trkStatusRe = regexp.MustCompile(`(?:^|[\s/\\"'])trk(?:\.exe)?["']?\s+statusline(?:\s|$)`)
)

// HookEvents are the Claude Code hook events trk init registers.
var HookEvents = []string{
	"SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"PostToolUseFailure", "PermissionRequest", "Notification", "Stop", "SubagentStop", "PreCompact",
}

func MergeHooks(root *Object, trkCmd string) (bool, error) {
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
	want := trkCmd + " hook"
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
		found, updated := updateTrkHook(groups, want)
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

func updateTrkHook(groups []any, want string) (found, updated bool) {
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
			if !trkHookRe.MatchString(cmd) {
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

func MergeStatusLine(root *Object, trkCmd string, chain bool) (StatusAction, error) {
	v, ok := root.Get("statusLine")
	if !ok || v == nil {
		sl := NewObject()
		sl.Set("type", "command")
		sl.Set("command", trkCmd+" statusline")
		root.Set("statusLine", sl)
		return StatusAdded, nil
	}
	sl, isObj := v.(*Object)
	if !isObj {
		return StatusKept, nil
	}
	cmd := ExistingStatusCommand(root)
	if trkStatusRe.MatchString(cmd) {
		return StatusPresent, nil
	}
	if cmd == "" || !chain {
		return StatusKept, nil
	}
	sl.Set("command", trkCmd+" statusline --then "+ShellQuote(cmd))
	return StatusChained, nil
}
