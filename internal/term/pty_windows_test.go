//go:build windows

package term

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWindowsTerminalInputResizeExitAndStop(t *testing.T) {
	if !Supported {
		t.Skip("ConPTY unavailable")
	}
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			dir := t.TempDir()
			literal := `quoted "value" & echo injected; $HOME ` + "日本語"
			cmd := exec.Command(os.Args[0], "-test.run=^TestConPTYHelper$", "--", literal)
			cmd.Dir = dir
			cmd.Env = append(CleanEnv(), "TRK_TEST_CONPTY=1")
			p, err := startPty(cmd, 100, 30)
			if err != nil {
				t.Fatal(err)
			}
			tm := &Term{f: p, info: Info{PID: p.Pid()}, subs: map[chan []byte]struct{}{}, ring: ring{max: scrollbackMax}}
			_, ch, detach := tm.Attach()
			defer detach()
			defer tm.kill()
			go tm.pump(func() {})
			if err := tm.Resize(80, 24); err != nil {
				t.Fatal(err)
			}
			if err := tm.Resize(65535, 24); err == nil {
				t.Fatal("overflow resize accepted")
			}
			if err := tm.Write([]byte("hello-trk\r\n")); err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			timer := time.NewTimer(12 * time.Second)
			defer timer.Stop()
			for !strings.Contains(out.String(), "REPLY:hello-trk") {
				select {
				case b, ok := <-ch:
					if !ok {
						t.Fatalf("closed early: %s", out.String())
					}
					out.Write(b)
				case <-timer.C:
					t.Fatalf("no echo: %s", out.String())
				}
			}
			encoded, _ := json.Marshal(literal)
			if !strings.Contains(out.String(), string(encoded)) {
				t.Fatalf("argv changed: %s", out.String())
			}
			if stop {
				go tm.kill()
			} else {
				_ = tm.Write([]byte("exit\r\n"))
			}
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						if !tm.Info().Exited {
							t.Fatal("exit not reported")
						}
						return
					}
				case <-timer.C:
					t.Fatal("terminal did not exit")
				}
			}
		})
	}
}

func TestConPTYHelper(t *testing.T) {
	if os.Getenv("TRK_TEST_CONPTY") != "1" {
		return
	}
	b, _ := json.Marshal(os.Args[len(os.Args)-1])
	fmt.Println(string(b))
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		if s.Text() == "exit" {
			os.Exit(0)
		}
		if s.Text() != "" {
			fmt.Println("REPLY:" + s.Text())
		}
	}
	os.Exit(0)
}
