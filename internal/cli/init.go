package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/GujaLomsadze/helm/internal/agent"
	_ "github.com/GujaLomsadze/helm/internal/agent/all"
	"github.com/GujaLomsadze/helm/internal/cfgfile"
	"github.com/GujaLomsadze/helm/internal/client"
	"github.com/GujaLomsadze/helm/internal/config"
	"golang.org/x/term"
)

func initCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("agent", agent.Default, "agent to connect: "+agentNames())
	yes := fs.Bool("yes", false, "accept defaults without prompting (chains an existing status line)")
	noStatus := fs.Bool("no-statusline", false, "leave the status line alone (Claude Code)")
	noMD := fs.Bool("no-instructions", false, "don't add the progress block to the agent's instruction file")
	mdPath := fs.String("instructions", "", "instruction file to edit (default: the agent's own, e.g. ~/.claude/CLAUDE.md, ~/.codex/AGENTS.md)")
	fs.BoolVar(noMD, "no-claude-md", false, "same as --no-instructions")
	fs.StringVar(mdPath, "claude-md", "", "same as --instructions")
	dry := fs.Bool("dry-run", false, "show what would change, write nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, ok := agent.Get(*name)
	if !ok {
		fmt.Fprintf(stderr, "hlm init: unknown agent %q (one of: %s)\n", *name, agentNames())
		return 2
	}
	in := bufio.NewReader(stdin)
	confirm := func(question string) bool {
		if *yes {
			return true
		}
		if !isTTY(stdin) {
			return false
		}
		u := newUI(stdout)
		fmt.Fprintf(stdout, "\n  %s %s %s ", u.warn("?"), question, u.dim("[Y/n]"))
		line, _ := in.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		return ans == "" || ans == "y" || ans == "yes"
	}
	res, err := a.Install(agent.InstallOptions{HlmCmd: hlmCommand(), DryRun: *dry, Instructions: *mdPath,
		SkipInstructions: *noMD, SkipStatusLine: *noStatus, Confirm: confirm, Now: time.Now()})
	if err != nil {
		fmt.Fprintf(stderr, "hlm init: %v\n", err)
		return 1
	}
	daemonUp := false
	if !*dry {
		resume()
		daemonUp = client.Default().EnsureDaemon(3 * time.Second)
	}
	renderInit(stdout, newUI(stdout), a.Label(), res, *dry, daemonUp, config.Port())
	renderOthers(stdout, newUI(stdout), a.Name())
	return 0
}

func agentNames() string {
	var n []string
	for _, a := range agent.All() {
		n = append(n, a.Name())
	}
	return strings.Join(n, ", ")
}

// renderOthers mentions other agents installed on this machine, so connecting them is one command.
func renderOthers(w io.Writer, u ui, done string) {
	for _, a := range agent.All() {
		if a.Name() == done {
			continue
		}
		if l, err := a.Launch(agent.LaunchSpec{}); err == nil {
			if _, err := exec.LookPath(l.Exe); err == nil {
				cmd := "hlm init"
				if a.Name() != agent.Default {
					cmd += " --agent " + a.Name()
				}
				fmt.Fprintf(w, "  %s %s is installed too: %s\n\n", u.dim("·"), a.Label(), u.accent(cmd))
			}
		}
	}
}

func isTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// hlmCommand returns the command hooks should run: an absolute, stable path.
func hlmCommand() string {
	exe, err := os.Executable()
	if err != nil {
		return "hlm"
	}
	if lp, err := exec.LookPath("hlm"); err == nil {
		if a, e1 := os.Stat(lp); e1 == nil {
			if b, e2 := os.Stat(exe); e2 == nil && os.SameFile(a, b) {
				if abs, err := filepath.Abs(lp); err == nil {
					exe = abs
				}
			}
		}
	}
	// Still the file named trk (updated from TRK)? Hooks call the hlm next to it.
	if b := strings.TrimSuffix(filepath.Base(exe), ".exe"); b == "trk" {
		sib := filepath.Join(filepath.Dir(exe), "hlm"+strings.TrimPrefix(filepath.Base(exe), "trk"))
		if a, e1 := os.Stat(sib); e1 == nil {
			if b, e2 := os.Stat(exe); e2 == nil && (os.SameFile(a, b) || a.Size() == b.Size()) {
				exe = sib
			}
		}
	}
	exe = filepath.ToSlash(exe)
	if strings.ContainsAny(exe, " '\"") {
		return cfgfile.ShellQuote(exe)
	}
	return exe
}
