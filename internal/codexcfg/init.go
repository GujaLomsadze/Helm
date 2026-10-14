// Package codexcfg installs opt-in Codex telemetry without editing config.toml or hook trust.
package codexcfg

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/GujaLomsadze/trk/internal/claudecfg"
	"github.com/GujaLomsadze/trk/internal/codex"
)

type Paths struct{ Hooks, AgentsMD string }
type Change struct{ Path, Backup, Content string }

func DefaultPaths(agentsMD string) (Paths, error) {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		dir = filepath.Join(home, ".codex")
	}
	return Paths{filepath.Join(dir, "hooks.json"), agentsMD}, nil
}

var ownHook = regexp.MustCompile(`(?:^|[\s/\\"'])trk(?:\.exe)?["']?\s+codex-hook\s*$`)

// Init validates every edit before writing. Each changed file gets an exclusive backup,
// followed by a same-directory atomic replacement. Errors identify the failing file.
func Init(paths Paths, command string, dry bool, now time.Time) ([]Change, error) {
	raw, err := read(paths.Hooks)
	if err != nil {
		return nil, err
	}
	root, err := claudecfg.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w (nothing changed)", paths.Hooks, err)
	}
	changed, err := merge(root, command)
	if err != nil {
		return nil, err
	}
	var changes []Change
	if changed {
		b, err := root.Marshal()
		if err != nil {
			return nil, err
		}
		changes = append(changes, Change{Path: paths.Hooks, Content: string(b)})
	}
	if paths.AgentsMD != "" {
		b, err := read(paths.AgentsMD)
		if err != nil {
			return nil, err
		}
		out, err := instructions(string(b))
		if err != nil {
			return nil, err
		}
		if out != string(b) {
			changes = append(changes, Change{Path: paths.AgentsMD, Content: out})
		}
	}
	if dry {
		return changes, nil
	}
	for i := range changes {
		c := &changes[i]
		original, err := os.ReadFile(c.Path)
		if err == nil {
			c.Backup = c.Path + ".trk-backup-" + now.Format("20060102-150405.000000000")
			f, err := os.OpenFile(c.Backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return changes[:i], fmt.Errorf("backup %s: %w", c.Path, err)
			}
			_, err = f.Write(original)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				return changes[:i], fmt.Errorf("backup %s: %w", c.Path, err)
			}
		} else if !os.IsNotExist(err) {
			return changes[:i], err
		}
		if err := writeAtomic(c.Path, []byte(c.Content)); err != nil {
			return changes[:i], fmt.Errorf("write %s (earlier files may have changed; backup %s): %w", c.Path, c.Backup, err)
		}
	}
	return changes, nil
}

func read(p string) ([]byte, error) {
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}

func merge(root *claudecfg.Object, command string) (bool, error) {
	v, exists := root.Get("hooks")
	hooks, ok := v.(*claudecfg.Object)
	if exists && !ok {
		return false, fmt.Errorf("hooks must be an object (nothing changed)")
	}
	if !exists {
		hooks = claudecfg.NewObject()
		root.Set("hooks", hooks)
	}
	changed := false
	for _, event := range codex.Events {
		v, exists := hooks.Get(event)
		groups, ok := v.([]any)
		if exists && !ok {
			return false, fmt.Errorf("hooks.%s must be an array (nothing changed)", event)
		}
		found := false
		for _, g := range groups {
			obj, ok := g.(*claudecfg.Object)
			if !ok {
				return false, fmt.Errorf("invalid %s group (nothing changed)", event)
			}
			hv, _ := obj.Get("hooks")
			handlers, ok := hv.([]any)
			if !ok {
				return false, fmt.Errorf("invalid %s handlers (nothing changed)", event)
			}
			for _, h := range handlers {
				ho, ok := h.(*claudecfg.Object)
				if !ok {
					return false, fmt.Errorf("invalid %s handler (nothing changed)", event)
				}
				cv, _ := ho.Get("command")
				cmd, _ := cv.(string)
				if cmd != command && !isOwnCommand(cmd) {
					continue
				}
				// A restrictive matcher is user-owned; do not silently widen it.
				if mv, exists := obj.Get("matcher"); exists {
					matcher, ok := mv.(string)
					if !ok {
						return false, fmt.Errorf("invalid %s matcher (nothing changed)", event)
					}
					if matcher != "" && matcher != "*" {
						continue
					}
				}
				found = true
				for k, want := range settings(event, command) {
					got, _ := ho.Get(k)
					if !reflect.DeepEqual(got, want) {
						ho.Set(k, want)
						changed = true
					}
				}
			}
		}
		if !found {
			h := claudecfg.NewObject()
			// Keep generated key order stable, including across repeated dry runs.
			s := settings(event, command)
			for _, k := range []string{"type", "command", "async", "timeout"} {
				h.Set(k, s[k])
			}
			g := claudecfg.NewObject()
			g.Set("hooks", []any{h})
			hooks.Set(event, append(groups, g))
			changed = true
		}
	}
	return changed, nil
}

func isOwnCommand(cmd string) bool {
	if ownHook.MatchString(cmd) {
		return true
	}
	encoded, ok := strings.CutPrefix(cmd, "powershell.exe -NoProfile -NonInteractive -EncodedCommand ")
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
	script = strings.TrimPrefix(script, "$ProgressPreference = 'SilentlyContinue'; ")
	return (strings.HasPrefix(script, "[Console]::In.ReadToEnd() | & '") || strings.HasPrefix(script, "$OutputEncoding = [Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false); [Console]::In.ReadToEnd() | & '")) && ownHook.MatchString(script)
}

func settings(event, command string) map[string]any {
	timeout := json.Number("3")
	return map[string]any{"type": "command", "command": command, "async": event != "SessionEnd", "timeout": timeout}
}

const begin = "<!-- trk:codex:begin -->"
const end = "<!-- trk:codex:end -->"
const snippet = begin + `
## TRK progress
Report declared intent with ` + "`trk codex`" + ` (best-effort telemetry; no need to check its output):
- Start: ` + "`trk codex start \"<task>\" --steps N`" + `
- Step: ` + "`trk codex step \"<work>\"`" + ` and ` + "`trk codex progress i/N`" + `
- Decision needed: ` + "`trk codex blocked \"<question>\"`" + `
- Finished: ` + "`trk codex done \"<result>\"`" + `
Native hook IDs identify observed activity. For declared progress, TRK_SESSION overrides
CODEX_THREAD_ID when available; without either, directory matching is best effort.
` + end + "\n"

func instructions(s string) (string, error) {
	b, e := strings.Index(s, begin), strings.Index(s, end)
	if (b >= 0 || e >= 0) && (b < 0 || e < b || strings.Count(s, begin) != 1 || strings.Count(s, end) != 1) {
		return "", fmt.Errorf("malformed TRK Codex instruction markers (nothing changed)")
	}
	if b >= 0 {
		return s[:b] + strings.TrimSuffix(snippet, "\n") + s[e+len(end):], nil
	}
	if strings.TrimSpace(s) == "" {
		return snippet, nil
	}
	return strings.TrimRight(s, "\n") + "\n\n" + snippet, nil
}

func writeAtomic(p string, b []byte) error {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(p); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".trk-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(f.Name(), mode); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}
