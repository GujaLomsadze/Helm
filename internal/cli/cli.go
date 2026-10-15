// Package cli implements every hlm subcommand.
package cli

import (
	"fmt"
	"io"

	"github.com/GujaLomsadze/helm/internal/version"
)

const usageText = `hlm — live tracker for coding agents

  hlm serve                      run daemon + dashboard (auto-spawned when needed)
  hlm open                       open the dashboard (resumes Helm if stopped)
  hlm stop                       stop Helm; hooks pause until hlm open
  hlm init                       connect Claude Code: hooks, status line, CLAUDE.md
  hlm init --agent codex         connect Codex: hooks, AGENTS.md
  hlm update [--check]           install the latest release (restarts the daemon)

  hlm start "<task>" --steps N   declare a task
  hlm step "<what>"              declare the current step
  hlm progress i/N               declare progress
  hlm blocked "<question>"       ask the human for a decision
  hlm done "<result>"            declare completion

  hlm hook [--agent codex]       (agents) forward hook JSON from stdin
  hlm statusline [--then CMD]    (Claude Code) forward status JSON, print a line
  hlm version
`

func usage(w io.Writer) { fmt.Fprint(w, usageText) }

// Run executes one hlm invocation and returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stdout)
		return 0
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "start", "step", "progress", "blocked", "done", "hook", "statusline":
		return safe(func() int {
			switch cmd {
			case "hook":
				return hook(rest, stdin, stdout)
			case "statusline":
				return statusline(rest, stdin, stdout)
			}
			return report(cmd, rest, stderr)
		})
	case "serve":
		return serve(stdout, stderr)
	case "open":
		return open(stdin, stdout, stderr)
	case "stop":
		return stopCmd(stdout, stderr)
	case "update":
		return updateCmd(rest, stdout, stderr)
	case "init":
		return initCmd(rest, stdin, stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "hlm %s\n", version.Version)
		return 0
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	}
	fmt.Fprintf(stderr, "hlm: unknown command %q (try `hlm help`)\n", cmd)
	return 2
}

// safe guarantees exit 0 for agent-facing commands, even on panic.
func safe(f func() int) (code int) {
	defer func() {
		if recover() != nil {
			code = 0
		}
	}()
	f()
	return 0
}

// RenamedNotice tells people who type the old name that TRK is now Helm. Only on the
// commands a person runs; hooks and agents' progress calls stay silent.
func RenamedNotice(args []string, w io.Writer) bool {
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "", "open", "init", "update", "stop", "serve", "version", "--version", "-v", "help", "--help", "-h":
		u := newUI(w)
		fmt.Fprintf(w, "\n  %s TRK is now %s. The command is %s; %s keeps working for now.\n    Run %s once to switch your hooks over.\n",
			u.warn("!"), u.bold("Helm"), u.accent("hlm"), u.dim("trk"), u.accent("hlm init"))
		return true
	}
	return false
}
