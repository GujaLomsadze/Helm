package cfgfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReadOptional reads a config file; a missing one is empty, not an error.
func ReadOptional(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

// Backup saves data next to path before TRK rewrites it.
func Backup(path string, data []byte, now time.Time) (string, error) {
	dst := path + ".trk-backup-" + now.Format("20060102-150405")
	return dst, os.WriteFile(dst, data, 0o600)
}

// WriteAtomic writes via temp file + rename, following symlinks (dotfile setups) and keeping the mode.
func WriteAtomic(path string, data []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".trk-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

const (
	BlockBegin = "<!-- trk:begin -->"
	BlockEnd   = "<!-- trk:end -->"
	Snippet    = BlockBegin + `
## Progress reporting
Report progress with the ` + "`trk`" + ` CLI. It never fails, prints nothing and always exits 0, so don't check its output.
- At the start of a task: ` + "`trk start \"<task>\" --steps <N>`" + `
- When moving to a new step: ` + "`trk step \"<what you're doing>\"`" + ` and ` + "`trk progress <i>/<N>`" + `
- When you need a human decision: ` + "`trk blocked \"<question>\"`" + `
- When finished: ` + "`trk done \"<one-line result>\"`" + `

Attach ` + "`trk`" + ` to a shell command you were going to run anyway, so it adds no extra step:
` + "`trk step \"Writing tests\"; go test ./...`" + `
Put ` + "`trk`" + ` first and join with ` + "`;`" + ` (never pipe it), so the real command's output and exit code are unchanged.
Run ` + "`trk`" + ` on its own only when there is no command to attach it to.
` + BlockEnd + "\n"
)

// ApplyBlock adds TRK's progress block to an instruction file, or replaces it in place.
func ApplyBlock(existing string) string {
	b, e := strings.Index(existing, BlockBegin), strings.Index(existing, BlockEnd)
	if b >= 0 && e > b {
		return existing[:b] + strings.TrimSuffix(Snippet, "\n") + existing[e+len(BlockEnd):]
	}
	if strings.TrimSpace(existing) == "" {
		return Snippet
	}
	return strings.TrimRight(existing, "\n") + "\n\n" + Snippet
}

// ShellQuote single-quotes s for a POSIX shell command line.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
