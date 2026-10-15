package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// Helm was called TRK, and its command was trk. An install updated from TRK can be
// running as "trk" with no "hlm" anywhere; AdoptNewName puts hlm next to it (a link,
// or a copy on Windows), so the new name works and `hlm init` points hooks at it.
// It reports the hlm path, or "" when it couldn't.
func AdoptNewName(w io.Writer) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	name := "hlm"
	if runtime.GOOS == "windows" {
		name = "hlm.exe"
	}
	dst := filepath.Join(filepath.Dir(exe), name)
	if _, err := os.Lstat(dst); err == nil {
		return dst // already there (installed, or linked earlier)
	}
	if runtime.GOOS == "windows" {
		b, err := os.ReadFile(exe)
		if err != nil || os.WriteFile(dst, b, 0o755) != nil {
			return ""
		}
	} else if os.Symlink(filepath.Base(exe), dst) != nil {
		return ""
	}
	fmt.Fprintf(w, "  %s added %s next to trk\n", newUI(w).ok("✓"), dst)
	return dst
}
