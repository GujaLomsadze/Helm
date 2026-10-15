package codex

import (
	"os/exec"
	"syscall"
)

func quietProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
