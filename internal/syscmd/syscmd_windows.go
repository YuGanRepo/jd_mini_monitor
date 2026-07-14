//go:build windows

// Package syscmd wraps os/exec so that external console programs (reg, certutil,
// powershell, ...) run without briefly flashing a console window on Windows.
package syscmd

import (
	"context"
	"os/exec"
	"syscall"
)

// createNoWindow is the Windows process creation flag CREATE_NO_WINDOW, which
// prevents a console window from being allocated for console-subsystem programs.
const createNoWindow = 0x08000000

func hide(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd
}

// Command is exec.Command with a hidden console window.
func Command(name string, args ...string) *exec.Cmd {
	return hide(exec.Command(name, args...))
}

// CommandContext is exec.CommandContext with a hidden console window.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return hide(exec.CommandContext(ctx, name, args...))
}
