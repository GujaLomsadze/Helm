package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/GujaLomsadze/helm/internal/agent"
	"golang.org/x/term"
)

// ui styles interactive output. Colour only on a real terminal, never with NO_COLOR.
type ui struct{ color bool }

func newUI(w io.Writer) ui {
	f, ok := w.(*os.File)
	return ui{color: ok && term.IsTerminal(int(f.Fd())) && os.Getenv("NO_COLOR") == ""}
}

func (u ui) paint(code, s string) string {
	if !u.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (u ui) accent(s string) string { return u.paint("1;38;2;255;90;74", s) } // phosphor red
func (u ui) ok(s string) string     { return u.paint("38;2;255;122;107", s) }
func (u ui) warn(s string) string   { return u.paint("38;2;255;178;122", s) } // amber
func (u ui) bold(s string) string   { return u.paint("1", s) }
func (u ui) dim(s string) string    { return u.paint("2", s) }

func (u ui) banner(w io.Writer, subtitle string) {
	fmt.Fprintf(w, "\n  %s  %s\n\n", u.accent("HELM.EXE"), u.dim(subtitle))
}

// row prints "  ✓ Label        detail".
func (u ui) row(w io.Writer, mark, label, detail string) {
	fmt.Fprintf(w, "  %s %s %s\n", mark, u.bold(fmt.Sprintf("%-12s", label)), detail)
}

func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return p
}

func renderInit(w io.Writer, u ui, label string, r agent.InstallResult, dry, daemonUp bool, port int) {
	if dry {
		u.banner(w, "dry run — nothing will be written")
	} else {
		u.banner(w, "connected to "+label)
	}
	for _, s := range r.Steps {
		detail := s.Detail
		if filepath.IsAbs(detail) {
			detail = tilde(detail)
			if s.State == agent.StepAdded && dry {
				detail = "would update " + detail
			} else if s.State == agent.StepAdded {
				detail = "updated " + detail
			}
		}
		switch s.State {
		case agent.StepAdded:
			u.row(w, u.ok("✓"), s.Label, detail)
		case agent.StepKept:
			u.row(w, u.warn("!"), s.Label, detail)
		default:
			u.row(w, u.dim("·"), s.Label, u.dim(detail))
		}
	}
	if dry {
		if r.Changed() {
			fmt.Fprintf(w, "\n  Run %s to apply.\n\n", u.accent("hlm init"+agentFlag(label)))
		} else {
			fmt.Fprintf(w, "\n  %s\n\n", u.dim("Nothing to change."))
		}
		return
	}
	if daemonUp {
		u.row(w, u.ok("✓"), "Daemon", "running")
	} else {
		u.row(w, u.warn("!"), "Daemon", "not started yet"+u.dim(" — starts on first use"))
	}
	var backups []string
	for _, s := range r.Steps {
		if s.Backup != "" && !slices.Contains(backups, s.Backup) {
			backups = append(backups, s.Backup)
		}
	}
	if len(backups) > 0 {
		fmt.Fprintf(w, "\n  %s\n", u.dim("Backups (restore these to undo)"))
		for _, b := range backups {
			fmt.Fprintf(w, "    %s\n", u.dim(tilde(b)))
		}
	}
	fmt.Fprintf(w, "\n  %s\n", u.bold("Next"))
	n := 1
	for _, step := range r.Next {
		fmt.Fprintf(w, "    %s  %s\n", u.accent(fmt.Sprint(n)), step)
		n++
	}
	fmt.Fprintf(w, "    %s  %s  %s\n\n", u.accent(fmt.Sprint(n)), u.accent("hlm open"), u.dim(fmt.Sprintf("→ http://localhost:%d", port)))
}

// agentFlag repeats --agent for a non-default agent in the "run this" hint.
func agentFlag(label string) string {
	for _, a := range agent.All() {
		if a.Label() == label && a.Name() != agent.Default {
			return " --agent " + a.Name()
		}
	}
	return ""
}
