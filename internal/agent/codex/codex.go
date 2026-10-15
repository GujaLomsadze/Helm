package codex

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/GujaLomsadze/trk/internal/agent"
)

func init() { agent.Register(Adapter{}) }

type Adapter struct{}

func (Adapter) Name() string  { return "codex" }
func (Adapter) Label() string { return "Codex" }

// Caps: one Codex process can host many sessions (its shared app server), so pids say
// nothing about sessions; a turn's end closes calls that never reported back.
func (Adapter) Caps() agent.Caps { return agent.Caps{StopEndsTools: true} }

// SessionFromEnv: Codex gives the commands it runs their thread id.
func (Adapter) SessionFromEnv(getenv func(string) string) string { return getenv("CODEX_THREAD_ID") }

// Launch: `codex --no-daemon [resume|fork <id>] [-- <prompt>]`. --no-daemon gives each
// terminal its own Codex process, so stopping it can't touch sessions TRK didn't start.
func (Adapter) Launch(sp agent.LaunchSpec) (agent.Launch, error) {
	a := []string{"--no-daemon"}
	if sp.Resume != "" {
		verb := "resume"
		if sp.Fork {
			verb = "fork"
		}
		a = append(a, verb, sp.Resume)
	}
	if p := strings.TrimSpace(sp.Prompt); p != "" {
		a = append(a, "--", p)
	}
	exe, err := Executable() // Args stay usable on error: tests run another program
	return agent.Launch{Exe: exe, Args: a}, err
}

// PrivateEnv: a codex that inherits these joins the launching thread instead of starting its own.
func (Adapter) PrivateEnv(k string) bool {
	switch k {
	case "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CODEX_APP_TOOLS_PIPE_PATH", "CODEX_INTERNAL_ORIGINATOR_OVERRIDE":
		return true
	}
	return false
}

func (Adapter) ResumeCommand(native string) string { return "codex resume " + native }

// Executable finds the native codex binary (TRK_CODEX_COMMAND overrides). On Windows npm
// installs a .cmd shim; TRK resolves the packaged codex.exe behind it rather than run a shell.
func Executable() (string, error) {
	if p := os.Getenv("TRK_CODEX_COMMAND"); p != "" {
		if found, err := exec.LookPath(p); err == nil {
			return found, nil
		}
		return "", errors.New("TRK_CODEX_COMMAND does not point to an executable")
	}
	if runtime.GOOS != "windows" {
		if p, err := exec.LookPath("codex"); err == nil {
			return p, nil
		}
	} else {
		if p, err := exec.LookPath("codex.exe"); err == nil {
			return p, nil
		}
		if shim, err := exec.LookPath("codex.cmd"); err == nil {
			base := filepath.Join(filepath.Dir(shim), "node_modules", "@openai", "codex")
			matches, _ := filepath.Glob(filepath.Join(base, "node_modules", "@openai", "codex-win32-*", "vendor", "*-pc-windows-msvc", "bin", "codex.exe"))
			if len(matches) == 1 {
				return matches[0], nil
			}
		}
	}
	return "", errors.New("codex is not on TRK's PATH; start TRK from a shell where `codex` works (trk stop, then trk open), or set TRK_CODEX_COMMAND")
}
