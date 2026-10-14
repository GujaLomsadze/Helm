package cli

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/GujaLomsadze/trk/internal/claudecfg"
	"github.com/GujaLomsadze/trk/internal/client"
	"github.com/GujaLomsadze/trk/internal/codexcfg"
	"github.com/GujaLomsadze/trk/internal/config"
	"golang.org/x/term"
)

func initCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "accept defaults without prompting (chains an existing status line)")
	noStatus := fs.Bool("no-statusline", false, "leave the status line alone")
	noMD := fs.Bool("no-claude-md", false, "don't add the CLAUDE.md block")
	mdPath := fs.String("claude-md", "", "CLAUDE.md to edit (default: <claude config dir>/CLAUDE.md)")
	dry := fs.Bool("dry-run", false, "show what would change, write nothing")
	agent := fs.String("agent", "claude", "integration to install: claude or codex")
	agentsMD := fs.String("agents-md", "", "optional AGENTS.md to add Codex progress instructions to")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *agent == "codex" {
		if *mdPath != "" {
			fmt.Fprintln(stderr, "use --agents-md with --agent codex")
			return 2
		}
		paths, err := codexcfg.DefaultPaths(*agentsMD)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		command, err := codexHookCommand()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "TRK Codex hooks forward JSON to this executable's codex-hook command.")
		changes, err := codexcfg.Init(paths, command, *dry, time.Now())
		if err != nil {
			fmt.Fprintln(stderr, "trk init: "+err.Error())
			return 1
		}
		for _, c := range changes {
			verb := "updated"
			if *dry {
				verb = "would update"
			}
			fmt.Fprintf(stdout, "%s %s\n", verb, c.Path)
			if c.Backup != "" {
				fmt.Fprintf(stdout, "backup: %s\n", c.Backup)
			}
			if *dry {
				fmt.Fprintf(stdout, "%s\n", c.Content)
			}
		}
		if len(changes) == 0 {
			fmt.Fprintln(stdout, "Codex integration already present.")
		}
		fmt.Fprintln(stdout, "Codex hooks require a hooks-capable release (verified CLI: 0.162.1). Restart Codex and review/trust the TRK entries with /hooks; then run trk open.")
		return 0
	}
	if *agent != "claude" || *agentsMD != "" {
		fmt.Fprintln(stderr, "use --agent claude or --agent codex; --agents-md is Codex-only")
		return 2
	}
	paths, err := claudecfg.DefaultPaths()
	if err != nil {
		fmt.Fprintf(stderr, "trk init: %v\n", err)
		return 1
	}
	if *mdPath != "" {
		paths.ClaudeMD = *mdPath
	}
	in := bufio.NewReader(stdin)
	chain := func(existing string) bool {
		if *yes {
			return true
		}
		if !isTTY(stdin) {
			return false
		}
		u := newUI(stdout)
		fmt.Fprintf(stdout, "\n  %s You already have a status line: %s\n    Chain it through trk? It keeps showing exactly as before. %s ", u.warn("?"), u.bold(existing), u.dim("[Y/n]"))
		line, _ := in.ReadString('\n')
		a := strings.ToLower(strings.TrimSpace(line))
		return a == "" || a == "y" || a == "yes"
	}
	res, err := claudecfg.Init(claudecfg.InitOptions{
		Paths: paths, TrkCmd: trkCommand(), ChainStatus: chain,
		SkipStatusLine: *noStatus, SkipClaudeMD: *noMD, DryRun: *dry, Now: time.Now(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "trk init: %v\n", err)
		return 1
	}
	daemonUp := false
	if !*dry {
		resume()
		daemonUp = client.Default().EnsureDaemon(3 * time.Second)
	}
	renderInit(stdout, newUI(stdout), res, paths, *dry, daemonUp, config.Port())
	return 0
}

// Windows hook commands can be launched through different shells. A fixed PowerShell
// invocation forwards JSON stdin explicitly and quotes the executable without shell expansion.
func codexHookCommand() (string, error) {
	if runtime.GOOS != "windows" {
		return trkCommand() + " codex-hook", nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	script := "$ProgressPreference = 'SilentlyContinue'; $OutputEncoding = [Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false); [Console]::In.ReadToEnd() | & '" + strings.ReplaceAll(exe, "'", "''") + "' codex-hook"
	units := utf16.Encode([]rune(script))
	b := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(b[i*2:], u)
	}
	return "powershell.exe -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(b), nil
}

func isTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// trkCommand returns the command hooks should run: an absolute, stable path.
func trkCommand() string {
	exe, err := os.Executable()
	if err != nil {
		return "trk"
	}
	if lp, err := exec.LookPath("trk"); err == nil {
		if a, e1 := os.Stat(lp); e1 == nil {
			if b, e2 := os.Stat(exe); e2 == nil && os.SameFile(a, b) {
				if abs, err := filepath.Abs(lp); err == nil {
					exe = abs
				}
			}
		}
	}
	exe = filepath.ToSlash(exe)
	if strings.ContainsAny(exe, " '\"") {
		return claudecfg.ShellQuote(exe)
	}
	return exe
}
