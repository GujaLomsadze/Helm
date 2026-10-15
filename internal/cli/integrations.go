package cli

import (
	"os"
	"strings"

	"bytes"
	"encoding/json"
	"fmt"
	"github.com/GujaLomsadze/trk/internal/agent"
	"io"
	"os/exec"
	"runtime"
	"time"

	"github.com/GujaLomsadze/trk/internal/model"
)

const maxStdin = 8 << 20

func readStdin(r io.Reader) []byte {
	b, _ := io.ReadAll(io.LimitReader(r, maxStdin+1))
	if len(b) > maxStdin {
		return nil
	}
	return b
}

// hook forwards one hook payload: `trk hook` (Claude Code) or `trk hook --agent codex`.
func hook(args []string, stdin io.Reader, stdout io.Writer) int {
	name := agent.Default
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--agent" && i+1 < len(args):
			name = args[i+1]
			i++
		case strings.HasPrefix(a, "--agent="):
			name = strings.TrimPrefix(a, "--agent=")
		}
	}
	ad, ok := agent.Get(name)
	b := readStdin(stdin)
	if !ok || len(b) == 0 || !json.Valid(b) {
		return 0
	}
	if reply := ad.HookReply(b); reply != "" {
		fmt.Fprintln(stdout, reply) // before the send: the agent may be waiting on it
	}
	e := envelope(model.SourceHook, "", b)
	e.TerminalID = os.Getenv("TRK_TERMINAL_ID")
	if name != agent.Default {
		e.Agent = name
	}
	if !ad.Caps().PIDAttribution {
		e.ClaudePID = 0 // its process says nothing about which session this is
	}
	send(e)
	return 0
}

func statusline(args []string, stdin io.Reader, stdout io.Writer) int {
	then := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--then" && i+1 < len(args) {
			then = args[i+1]
			i++
		}
	}
	b := readStdin(stdin)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if len(b) > 0 && json.Valid(b) {
			send(envelope(model.SourceStatusline, "", b))
		}
	}()
	if then != "" {
		runThen(then, b, stdout)
	} else {
		fmt.Fprintln(stdout, compactLine(b))
	}
	<-done
	return 0
}

func runThen(cmdline string, input []byte, stdout io.Writer) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		if bash, err := exec.LookPath("bash"); err == nil {
			cmd = exec.Command(bash, "-c", cmdline)
		} else {
			cmd = exec.Command("cmd", "/C", cmdline)
		}
	} else {
		cmd = exec.Command("sh", "-c", cmdline)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(input), stdout, io.Discard
	_ = cmd.Run()
}

func compactLine(raw []byte) string {
	type win struct {
		UsedPercentage *float64 `json:"used_percentage"`
		ResetsAt       *float64 `json:"resets_at"`
	}
	var p struct {
		ContextWindow *struct {
			UsedPercentage *float64 `json:"used_percentage"`
		} `json:"context_window"`
		RateLimits *struct {
			FiveHour *win `json:"five_hour"`
			SevenDay *win `json:"seven_day"`
		} `json:"rate_limits"`
	}
	_ = json.Unmarshal(raw, &p)
	pct := func(v *float64) string {
		if v == nil {
			return "—"
		}
		return fmt.Sprintf("%.0f%%", *v)
	}
	w := func(x *win) string {
		if x == nil || (x.ResetsAt != nil && int64(*x.ResetsAt) < time.Now().Unix()) {
			return "—"
		}
		return pct(x.UsedPercentage)
	}
	var ctx *float64
	if p.ContextWindow != nil {
		ctx = p.ContextWindow.UsedPercentage
	}
	var five, seven *win
	if p.RateLimits != nil {
		five, seven = p.RateLimits.FiveHour, p.RateLimits.SevenDay
	}
	return fmt.Sprintf("trk · ctx %s · 5h %s · 7d %s", pct(ctx), w(five), w(seven))
}
