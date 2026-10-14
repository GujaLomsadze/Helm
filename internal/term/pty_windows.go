//go:build windows

package term

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/UserExistsError/conpty"
)

type windowsTerminal struct {
	*conpty.ConPty
	once sync.Once
	done chan struct{}
	err  error
}

func (p *windowsTerminal) Close() error {
	p.once.Do(func() { _ = p.ConPty.Close() })
	return nil
}
func (p *windowsTerminal) Wait() error { <-p.done; return p.err }
func (p *windowsTerminal) Resize(cols, rows uint16) error {
	return p.ConPty.Resize(int(cols), int(rows))
}
func startPty(cmd *exec.Cmd, cols, rows uint16) (terminalProcess, error) {
	if !Supported {
		return nil, ErrUnsupported
	}
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	if strings.HasSuffix(strings.ToLower(cmd.Path), ".cmd") || strings.HasSuffix(strings.ToLower(cmd.Path), ".bat") {
		return nil, errors.New("install the agent's native Windows executable; shell shims are not supported")
	}
	// CreateProcess receives escaped argv, never a shell expression.
	a := append([]string{cmd.Path}, cmd.Args[1:]...)
	for i := range a {
		a[i] = syscall.EscapeArg(a[i])
	}
	c, err := conpty.Start(strings.Join(a, " "), conpty.ConPtyDimensions(int(cols), int(rows)), conpty.ConPtyWorkDir(cmd.Dir), conpty.ConPtyEnv(cmd.Env))
	if err != nil {
		return nil, err
	}
	p := &windowsTerminal{ConPty: c, done: make(chan struct{})}
	go func() {
		_, p.err = c.Wait(context.Background())
		// The reader drains output concurrently with ClosePseudoConsole.
		_ = p.Close()
		close(p.done)
	}()
	return p, nil
}

var Supported = conpty.IsConPtyAvailable()
