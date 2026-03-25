//go:build windows

package agents

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/UserExistsError/conpty"
)

// conptyProcess wraps *conpty.ConPty to implement processHandle.
type conptyProcess struct {
	cpty *conpty.ConPty
}

func (p *conptyProcess) Kill() error {
	return p.cpty.Close()
}

func (p *conptyProcess) Wait() error {
	_, err := p.cpty.Wait(context.Background())
	return err
}

// NewPTYSession starts a command in a new ConPTY on Windows.
// unsetEnv lists environment variable names to strip; extraEnv lists KEY=val
// entries to add.
func NewPTYSession(name, dir string, unsetEnv, extraEnv []string, command string, args ...string) (*PTYSession, error) {
	// ConPTY takes a single command line string.
	cmdLine := command
	if len(args) > 0 {
		cmdLine = command + " " + strings.Join(args, " ")
	}

	env := buildEnv(unsetEnv, extraEnv)

	cpty, err := conpty.Start(cmdLine,
		conpty.ConPtyWorkDir(dir),
		conpty.ConPtyEnv(env),
		conpty.ConPtyDimensions(120, 40),
	)
	if err != nil {
		return nil, fmt.Errorf("conpty start: %w", err)
	}

	buf := &outputBuffer{}

	// Continuously copy ConPTY output into the buffer.
	go func() {
		_, _ = io.Copy(buf, cpty)
	}()

	return &PTYSession{
		name:    name,
		writer:  cpty,
		closer:  cpty,
		buf:     buf,
		process: &conptyProcess{cpty: cpty},
	}, nil
}
