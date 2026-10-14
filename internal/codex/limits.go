package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type LimitWindow struct {
	UsedPercent  *float64 `json:"usedPercent"`
	DurationMins *int64   `json:"windowDurationMins"`
	ResetsAt     *int64   `json:"resetsAt"` // Unix seconds, as returned by Codex
}

type LimitBucket struct {
	ID        string       `json:"limitId"`
	Name      *string      `json:"limitName"`
	Primary   *LimitWindow `json:"primary"`
	Secondary *LimitWindow `json:"secondary"`
}

// ReadLimits owns only a short-lived stdio app-server process. No thread, turn,
// login, quota reset, or shared-daemon shutdown methods are sent.
func ReadLimits(ctx context.Context) ([]LimitBucket, error) {
	exe, err := codexExecutable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "app-server", "--listen", "stdio://")
	quietProcess(cmd)
	buckets, err := readLimitsProcess(cmd)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, errors.New("Codex usage refresh timed out; TRK will retry automatically")
	}
	return buckets, err
}

func codexExecutable() (string, error) {
	return Executable()
}

// Executable resolves the native CLI without invoking an npm shell shim.
func Executable() (string, error) {
	if p := os.Getenv("TRK_CODEX_COMMAND"); p != "" {
		if found, err := exec.LookPath(p); err == nil {
			return found, nil
		}
		return "", errors.New("TRK_CODEX_COMMAND does not point to an executable")
	}
	if runtime.GOOS != "windows" {
		return exec.LookPath("codex")
	}
	if p, err := exec.LookPath("codex.exe"); err == nil {
		return p, nil
	}
	// npm exposes a .cmd shim on Windows. Resolve the native packaged executable
	// rather than passing a command string through a shell.
	if shim, err := exec.LookPath("codex.cmd"); err == nil {
		base := filepath.Join(filepath.Dir(shim), "node_modules", "@openai", "codex")
		matches, _ := filepath.Glob(filepath.Join(base, "node_modules", "@openai", "codex-win32-*", "vendor", "*-pc-windows-msvc", "bin", "codex.exe"))
		if len(matches) == 1 {
			return matches[0], nil
		}
	}
	return "", errors.New("Codex CLI not found; install Codex or set TRK_CODEX_COMMAND to its native executable, then restart TRK")
}

func readLimitsProcess(cmd *exec.Cmd) ([]LimitBucket, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.New("Could not open Codex input")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, errors.New("Could not open Codex output")
	}
	cmd.Stderr = io.Discard // upstream errors may include account details
	cmd.WaitDelay = 250 * time.Millisecond
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, errors.New("Could not start Codex App Server")
	}
	defer func() { stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	send := json.NewEncoder(stdin)
	if err := send.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "trk", "title": "TRK limits", "version": "1"}}}); err != nil {
		return nil, errors.New("Could not initialize Codex")
	}
	scanner := bufio.NewScanner(io.LimitReader(stdout, 4<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	initialized := false
	for scanner.Scan() {
		var msg struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if msg.Method != "" {
			continue
		} // notifications never become account readings
		if msg.ID != 1 && msg.ID != 2 {
			continue
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return nil, errors.New("Codex could not read usage; check codex login status and use a ChatGPT account with quota access")
		}
		if msg.ID == 1 && !initialized {
			initialized = true
			if send.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}) != nil || send.Encode(map[string]any{"id": 2, "method": "account/rateLimits/read"}) != nil {
				return nil, errors.New("Could not request Codex usage")
			}
		} else if msg.ID == 2 && initialized {
			return decodeLimits(msg.Result)
		}
	}
	return nil, errors.New("Codex App Server closed before returning usage; TRK will retry automatically")
}

func decodeLimits(raw json.RawMessage) ([]LimitBucket, error) {
	var r struct {
		Legacy  *LimitBucket           `json:"rateLimits"`
		Buckets map[string]LimitBucket `json:"rateLimitsByLimitId"`
	}
	if json.Unmarshal(raw, &r) != nil || strings.TrimSpace(string(raw)) == "null" {
		return nil, errors.New("Codex returned an invalid usage response")
	}
	out := []LimitBucket{}
	if len(r.Buckets) > 0 {
		for id, b := range r.Buckets {
			b.ID = id
			out = append(out, b)
		}
	} else if r.Legacy != nil {
		b := *r.Legacy
		if b.ID == "" {
			b.ID = "codex"
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
