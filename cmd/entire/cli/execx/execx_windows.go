//go:build windows

package execx

import "os/exec"

// detachFromTTY is a no-op on Windows. Windows has no /dev/tty concept;
// interactive.CanPromptInteractively() already returns false there because
// the /dev/tty probe fails.
func detachFromTTY(_ *exec.Cmd) {}
