//go:build windows

package hooks

import (
	"os/exec"
	"syscall"
)

// hookProcAttr: no process groups on Windows; the shell itself is killed.
func hookProcAttr() *syscall.SysProcAttr { return nil }

func killGroup(*exec.Cmd) {}
