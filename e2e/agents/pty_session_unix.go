//go:build unix

package agents

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/creack/pty"
)

// NewPTYSession starts a command in a new PTY on Unix.
// unsetEnv lists environment variable names to strip; extraEnv lists KEY=val
// entries to add.
func NewPTYSession(name, dir string, unsetEnv, extraEnv []string, command string, args ...string) (*PTYSession, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = buildEnv(unsetEnv, extraEnv)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}

	buf := &outputBuffer{}

	// Continuously copy PTY output into the buffer.
	go func() {
		_, _ = io.Copy(buf, ptmx)
	}()

	return &PTYSession{
		name:    name,
		writer:  ptmx,
		closer:  ptmx,
		buf:     buf,
		process: &cmdProcess{cmd: cmd},
	}, nil
}
