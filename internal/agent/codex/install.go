package codex

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf16"

	"github.com/GujaLomsadze/helm/internal/agent"
	"github.com/GujaLomsadze/helm/internal/cfgfile"
)

// Paths are the files `hlm init --agent codex` edits. config.toml and hook trust are
// never touched: the user reviews Helm's hooks in Codex's /hooks screen.
type Paths struct{ Hooks, AgentsMD string }

func DefaultPaths() (Paths, error) {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		dir = filepath.Join(home, ".codex")
	}
	return Paths{Hooks: filepath.Join(dir, "hooks.json"), AgentsMD: filepath.Join(dir, "AGENTS.md")}, nil
}

func (Adapter) Install(o agent.InstallOptions) (agent.InstallResult, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return agent.InstallResult{}, err
	}
	if o.Instructions != "" {
		paths.AgentsMD = o.Instructions
	}
	if o.SkipInstructions {
		paths.AgentsMD = ""
	}
	cmd, err := hookCommand(o.HlmCmd)
	if err != nil {
		return agent.InstallResult{}, err
	}
	return install(paths, cmd, o)
}

type change struct {
	label, path string
	old, new    []byte
	existed     bool
}

// install checks every file before writing any of them, so bad JSON in hooks.json leaves
// AGENTS.md alone too.
func install(paths Paths, cmd string, o agent.InstallOptions) (agent.InstallResult, error) {
	var res agent.InstallResult
	var changes []change
	raw, existed, err := cfgfile.ReadOptional(paths.Hooks)
	if err != nil {
		return res, err
	}
	root, err := cfgfile.Parse(raw)
	if err != nil {
		return res, fmt.Errorf("%s: %v (nothing was changed)", paths.Hooks, err)
	}
	changed, err := mergeHooks(root, cmd)
	if err != nil {
		return res, fmt.Errorf("%s: %v (nothing was changed)", paths.Hooks, err)
	}
	if changed {
		out, err := root.Marshal()
		if err != nil {
			return res, err
		}
		changes = append(changes, change{"Hooks", paths.Hooks, raw, out, existed})
	} else {
		res.Steps = append(res.Steps, agent.InstallStep{Label: "Hooks", State: agent.StepPresent, Detail: "already set up"})
	}
	if paths.AgentsMD != "" {
		md, existed, err := cfgfile.ReadOptional(paths.AgentsMD)
		if err != nil {
			return res, err
		}
		if out := cfgfile.ApplyBlock(string(md)); out != string(md) {
			changes = append(changes, change{"AGENTS.md", paths.AgentsMD, md, []byte(out), existed})
		} else {
			res.Steps = append(res.Steps, agent.InstallStep{Label: "AGENTS.md", State: agent.StepPresent, Detail: "already set up"})
		}
	}
	for _, c := range changes {
		st := agent.InstallStep{Label: c.label, State: agent.StepAdded, Detail: c.path}
		if !o.DryRun {
			if c.existed {
				if st.Backup, err = cfgfile.Backup(c.path, c.old, o.Now); err != nil {
					return res, err
				}
			}
			if err := cfgfile.WriteAtomic(c.path, c.new); err != nil {
				return res, fmt.Errorf("write %s: %w", c.path, err)
			}
		}
		res.Steps = append(res.Steps, st)
	}
	res.Next = []string{"Restart Codex, then review and trust the Helm hooks in /hooks"}
	return res, nil
}

// ownHook matches Helm's Codex hook, current and from the first Codex release (codex-hook).
// TRK (Helm's old name) and its first Codex release (codex-hook) count as ours too: init rewrites them.
var ownHook = regexp.MustCompile(`(?:^|[\s/\\"'])(?:hlm|trk)(?:\.exe)?["']?\s+(?:hook\s+--agent[ =]codex|codex-hook)\s*$`)

func mergeHooks(root *cfgfile.Object, cmd string) (bool, error) {
	v, exists := root.Get("hooks")
	hooks, ok := v.(*cfgfile.Object)
	if exists && v != nil && !ok {
		return false, fmt.Errorf(`"hooks" is not an object; refusing to edit`)
	}
	if hooks == nil {
		hooks = cfgfile.NewObject()
		root.Set("hooks", hooks)
	}
	changed := false
	for _, event := range Events {
		v, exists := hooks.Get(event)
		groups, ok := v.([]any)
		if exists && v != nil && !ok {
			return false, fmt.Errorf("hooks.%s is not an array; refusing to edit", event)
		}
		found := false
		for _, g := range groups {
			obj, ok := g.(*cfgfile.Object)
			if !ok {
				return false, fmt.Errorf("hooks.%s has an entry that is not an object; refusing to edit", event)
			}
			hv, _ := obj.Get("hooks")
			handlers, ok := hv.([]any)
			if !ok {
				return false, fmt.Errorf("hooks.%s has an entry without a hooks list; refusing to edit", event)
			}
			for _, h := range handlers {
				ho, ok := h.(*cfgfile.Object)
				if !ok {
					return false, fmt.Errorf("hooks.%s has a handler that is not an object; refusing to edit", event)
				}
				cv, _ := ho.Get("command")
				c, _ := cv.(string)
				if c != cmd && !isOwnCommand(c) {
					continue
				}
				// A narrowing matcher is the user's choice; leave that entry alone.
				if mv, has := obj.Get("matcher"); has {
					m, ok := mv.(string)
					if !ok {
						return false, fmt.Errorf("hooks.%s has a matcher that is not a string; refusing to edit", event)
					}
					if m != "" && m != "*" {
						continue
					}
				}
				found = true
				for k, want := range handler(event, cmd) {
					if got, _ := ho.Get(k); !reflect.DeepEqual(got, want) {
						ho.Set(k, want)
						changed = true
					}
				}
			}
		}
		if !found {
			h := cfgfile.NewObject()
			s := handler(event, cmd)
			for _, k := range []string{"type", "command", "async", "timeout"} { // stable key order
				h.Set(k, s[k])
			}
			g := cfgfile.NewObject()
			g.Set("hooks", []any{h})
			hooks.Set(event, append(groups, g))
			changed = true
		}
	}
	return changed, nil
}

// handler: async so Codex never waits on Helm, except SessionEnd, which Codex runs synchronously.
func handler(event, cmd string) map[string]any {
	return map[string]any{"type": "command", "command": cmd, "async": event != "SessionEnd", "timeout": json.Number("3")}
}

// psPrefix: Windows hook commands can run under different shells. A fixed PowerShell
// invocation forwards JSON stdin as UTF-8 and quotes the executable without expansion.
const psPrefix = "powershell.exe -NoProfile -NonInteractive -EncodedCommand "

func hookCommand(hlmCmd string) (string, error) {
	if runtime.GOOS != "windows" {
		return hlmCmd + " hook --agent codex", nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	script := "$ProgressPreference = 'SilentlyContinue'; $OutputEncoding = [Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false); [Console]::In.ReadToEnd() | & '" + strings.ReplaceAll(exe, "'", "''") + "' hook --agent codex"
	units := utf16.Encode([]rune(script))
	b := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(b[i*2:], u)
	}
	return psPrefix + base64.StdEncoding.EncodeToString(b), nil
}

func isOwnCommand(cmd string) bool {
	if ownHook.MatchString(cmd) {
		return true
	}
	encoded, ok := strings.CutPrefix(cmd, psPrefix)
	if !ok {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(b)%2 != 0 {
		return false
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	script := string(utf16.Decode(units))
	return strings.Contains(script, "[Console]::In.ReadToEnd() | & '") && ownHook.MatchString(script)
}
