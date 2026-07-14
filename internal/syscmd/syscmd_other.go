//go:build !windows

// Package syscmd wraps os/exec. On non-Windows platforms it is a thin
// passthrough; the Windows build hides the console window.
package syscmd

import (
	"context"
	"os/exec"
)

// Command is exec.Command.
func Command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// CommandContext is exec.CommandContext.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
