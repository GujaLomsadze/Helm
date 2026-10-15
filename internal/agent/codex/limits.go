package codex

import (
	"strconv"

	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/GujaLomsadze/helm/internal/agent"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type rawWindow struct {
	UsedPercent  *float64 `json:"usedPercent"`
	DurationMins *int64   `json:"windowDurationMins"`
	ResetsAt     *int64   `json:"resetsAt"` // Unix seconds, as returned by Codex
}

type LimitBucket struct {
	ID        string     `json:"limitId"`
	Name      *string    `json:"limitName"`
	Primary   *rawWindow `json:"primary"`
	Secondary *rawWindow `json:"secondary"`
}

// Limits reads the signed-in account's plan usage (agent.LimitsReader).
func (Adapter) Limits(ctx context.Context) ([]agent.LimitWindow, error) {
	buckets, err := ReadLimits(ctx)
	if err != nil {
		return nil, err
	}
	var out []agent.LimitWindow
	for _, b := range buckets {
		name := "Codex"
		if b.Name != nil && *b.Name != "" {
			name = *b.Name
		} else if b.ID != "codex" {
			name = "Codex · " + b.ID
		}
		for i, w := range []*rawWindow{b.Primary, b.Secondary} {
			if w == nil {
				continue
			}
			lw := agent.LimitWindow{ID: b.ID + []string{"/primary", "/secondary"}[i], Label: windowLabel(name, w.DurationMins), UsedPct: w.UsedPercent}
			if w.DurationMins != nil {
				lw.DurationMin = *w.DurationMins
			}
			if w.ResetsAt != nil {
				lw.ResetAt = *w.ResetsAt * 1000
			}
			out = append(out, lw)
		}
	}
	return out, nil
}

func windowLabel(name string, mins *int64) string {
	prefix := ""
	if name != "Codex" {
		prefix = name + " · "
	}
	switch {
	case mins == nil || *mins <= 0:
		return prefix + "limit"
	case *mins == 300:
		return prefix + "5-hour"
	case *mins == 10080:
		return prefix + "Weekly"
	case *mins%1440 == 0:
		return prefix + strconv.FormatInt(*mins/1440, 10) + "-day"
	case *mins%60 == 0:
		return prefix + strconv.FormatInt(*mins/60, 10) + "-hour"
	}
	return prefix + strconv.FormatInt(*mins, 10) + "-minute"
}

// ReadLimits owns only a short-lived stdio app-server process. No thread, turn,
// login, quota reset, or shared-daemon shutdown methods are sent.
func ReadLimits(ctx context.Context) ([]LimitBucket, error) {
	exe, err := Executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "app-server", "--listen", "stdio://")
	quietProcess(cmd)
	buckets, err := readLimitsProcess(cmd)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, errors.New("Codex usage refresh timed out; Helm will retry automatically")
	}
	return buckets, err
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
	if err := send.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "hlm", "title": "Helm limits", "version": "1"}}}); err != nil {
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
	return nil, errors.New("Codex App Server closed before returning usage; Helm will retry automatically")
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
