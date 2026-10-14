//go:build !windows

package codex

import "os/exec"

func quietProcess(cmd *exec.Cmd) {}
