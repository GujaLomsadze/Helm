package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDecodeLimits(t *testing.T) {
	raw := json.RawMessage(`{"rateLimits":{"limitId":"legacy","primary":{"usedPercent":99}},"rateLimitsByLimitId":{"codex":{"limitName":null,"primary":{"usedPercent":0,"windowDurationMins":10080,"resetsAt":12345},"secondary":null},"extra":{"primary":{"usedPercent":null,"windowDurationMins":15}}},"private":"not exposed"}`)
	b, err := decodeLimits(raw)
	if err != nil || len(b) != 2 || b[0].ID != "codex" || *b[0].Primary.UsedPercent != 0 || *b[0].Primary.DurationMins != 10080 || b[0].Secondary != nil || b[1].Primary.UsedPercent != nil {
		t.Fatalf("%+v %v", b, err)
	}
	out, _ := json.Marshal(b)
	if strings.Contains(string(out), "private") || strings.Contains(string(out), "legacy") {
		t.Fatal(string(out))
	}
	b, err = decodeLimits(json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":12,"windowDurationMins":300}}}`))
	if err != nil || len(b) != 1 || b[0].ID != "codex" {
		t.Fatalf("%+v %v", b, err)
	}
	for _, raw := range []string{"null", "[]", "invalid"} {
		if _, err := decodeLimits(json.RawMessage(raw)); err == nil {
			t.Fatal("accepted " + raw)
		}
	}
}

func TestLimitsProcess(t *testing.T) {
	for _, mode := range []string{"ok", "rpc-error", "invalid", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			deadline := 3 * time.Second
			if mode == "timeout" {
				deadline = 250 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLimitsProcessHelper$")
			cmd.Env = append(os.Environ(), "HLM_LIMIT_HELPER="+mode)
			quietProcess(cmd)
			start := time.Now()
			b, err := readLimitsProcess(cmd)
			if mode == "ok" {
				if err != nil || len(b) != 1 || *b[0].Primary.UsedPercent != 7 {
					t.Fatalf("%+v %v", b, err)
				}
			} else if err == nil {
				t.Fatal("expected failure")
			}
			if err != nil && strings.Contains(err.Error(), "private-token") {
				t.Fatal("leaked upstream error")
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("subprocess exceeded deadline")
			}
		})
	}
}

func TestLimitsProcessHelper(t *testing.T) {
	mode := os.Getenv("HLM_LIMIT_HELPER")
	if mode == "" {
		return
	}
	if mode == "timeout" {
		time.Sleep(20 * time.Second)
		os.Exit(1)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for _, want := range []string{"initialize", "initialized", "account/rateLimits/read"} {
		if !scanner.Scan() {
			os.Exit(2)
		}
		var m struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &m) != nil || m.Method != want {
			os.Exit(3)
		}
		if want == "initialize" {
			fmt.Println(`{"method":"account/updated","params":{"private":"ignore"}}`)
			fmt.Println(`{"id":1,"result":{}}`)
		}
	}
	switch mode {
	case "ok":
		fmt.Println(`{"id":2,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":7,"windowDurationMins":10080}}}}`)
	case "rpc-error":
		fmt.Println(`{"id":2,"error":{"code":-1,"message":"private-token"}}`)
	case "invalid":
		fmt.Println(`{"id":2,"result":[]}`)
	}
	// Verify the caller terminates its owned process instead of waiting forever.
	time.Sleep(20 * time.Second)
	os.Exit(0)
}
