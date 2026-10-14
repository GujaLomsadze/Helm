//go:build !windows

package term

import (
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

type unixTerminal struct {
	*os.File
	cmd  *exec.Cmd
	once sync.Once
}

func (p *unixTerminal) Pid() int    { return p.cmd.Process.Pid }
func (p *unixTerminal) Wait() error { return p.cmd.Wait() }
func (p *unixTerminal) Close() error {
	p.once.Do(func() { _ = p.cmd.Process.Kill(); _ = p.File.Close() })
	return nil
}
func startPty(cmd *exec.Cmd, cols, rows uint16) (terminalProcess, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	return &unixTerminal{File: f, cmd: cmd}, nil
}
func (p *unixTerminal) Resize(cols, rows uint16) error {
	return pty.Setsize(p.File, &pty.Winsize{Cols: cols, Rows: rows})
}

// Supported reports whether this build can run terminals.
const Supported = true
