package agents

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// PTYSession implements Session using a native PTY (no tmux dependency).
// Platform-specific NewPTYSession constructors are in pty_session_unix.go
// and pty_session_windows.go.
type PTYSession struct {
	name         string
	writer       io.Writer // write end of PTY
	closer       io.Closer // PTY fd or ConPTY handle to close
	buf          *outputBuffer
	process      processHandle
	stableAtSend string   // stable content snapshot when Send was last called
	cleanups     []func() // run on Close
}

// processHandle abstracts process cleanup across platforms.
// On Unix this wraps *exec.Cmd; on Windows it wraps the ConPTY process.
type processHandle interface {
	Kill() error
	Wait() error
}

// cmdProcess wraps *exec.Cmd to implement processHandle.
type cmdProcess struct {
	cmd *exec.Cmd
}

func (p *cmdProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p *cmdProcess) Wait() error {
	return p.cmd.Wait()
}

// outputBuffer is a thread-safe, append-only buffer for PTY output.
type outputBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *outputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}

func (b *outputBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

// OnClose registers a function to run when the session is closed.
func (s *PTYSession) OnClose(fn func()) {
	s.cleanups = append(s.cleanups, fn)
}

func (s *PTYSession) Send(input string) error {
	preSend := stableContent(s.Capture())

	// Write the input text, then Enter. Sleep between them because
	// some agent TUIs swallow Enter if it arrives too quickly.
	if _, err := fmt.Fprint(s.writer, input); err != nil {
		return fmt.Errorf("pty write text: %w", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := fmt.Fprint(s.writer, "\r"); err != nil {
		return fmt.Errorf("pty write enter: %w", err)
	}

	// Wait for the terminal to reflect the echoed input, then snapshot.
	// This ensures WaitFor compares against post-echo content, preventing
	// false matches on prompt characters (e.g. ❯) in the echoed input.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		current := stableContent(s.Capture())
		if current != preSend {
			s.stableAtSend = current
			return nil
		}
	}
	s.stableAtSend = stableContent(s.Capture())
	return nil
}

// ansiRe strips ANSI escape sequences from terminal output.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func (s *PTYSession) Capture() string {
	raw := s.buf.String()
	// Strip ANSI escape sequences — PTY output includes them unlike tmux capture-pane.
	clean := ansiRe.ReplaceAllString(raw, "")
	return strings.TrimRight(clean, "\n")
}

const (
	settleTime   = 2 * time.Second
	pollInterval = 500 * time.Millisecond
)

// stableContent returns the content with the last few lines stripped,
// so that TUI status bar updates don't prevent the settle timer.
func stableContent(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 3 {
		lines = lines[:len(lines)-3]
	}
	return strings.Join(lines, "\n")
}

func (s *PTYSession) WaitFor(pattern string, timeout time.Duration) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	deadline := time.Now().Add(timeout)
	var matchedAt time.Time
	var lastStable string
	contentChanged := s.stableAtSend == "" // skip change requirement for initial waits

	for time.Now().Before(deadline) {
		content := s.Capture()
		stable := stableContent(content)

		if !re.MatchString(content) {
			// Pattern lost — reset
			matchedAt = time.Time{}
			lastStable = ""
			time.Sleep(pollInterval)
			continue
		}

		// Detect content change since Send was called
		if !contentChanged && stable != s.stableAtSend {
			contentChanged = true
		}

		if stable != lastStable {
			// Pattern matches but content is still changing — reset settle timer
			matchedAt = time.Now()
			lastStable = stable
			time.Sleep(pollInterval)
			continue
		}

		// Pattern matches and content hasn't changed since matchedAt.
		// Only settle if content changed at least once after Send
		// (prevents false settle on echoed input before agent starts).
		if contentChanged && time.Since(matchedAt) >= settleTime {
			return content, nil
		}

		time.Sleep(pollInterval)
	}
	content := s.Capture()
	return content, fmt.Errorf("timed out waiting for %q after %s\n--- pane content ---\n%s\n--- end pane content ---", pattern, timeout, content)
}

// SendKeys sends raw key names to the PTY, translating tmux key names
// (e.g. "Enter", "Down", "Up", "Escape") to their terminal escape sequences.
func (s *PTYSession) SendKeys(keys ...string) error {
	for _, key := range keys {
		var seq string
		switch key {
		case "Enter":
			seq = "\r"
		case "Down":
			seq = "\x1b[B"
		case "Up":
			seq = "\x1b[A"
		case "Left":
			seq = "\x1b[D"
		case "Right":
			seq = "\x1b[C"
		case "Escape":
			seq = "\x1b"
		case "Tab":
			seq = "\t"
		case "BSpace":
			seq = "\x7f"
		case "Space":
			seq = " "
		case "C-c":
			seq = "\x03"
		default:
			// For literal text, send as-is.
			seq = key
		}
		if _, err := fmt.Fprint(s.writer, seq); err != nil {
			return fmt.Errorf("pty send key %q: %w", key, err)
		}
	}
	return nil
}

func (s *PTYSession) Close() error {
	for _, fn := range s.cleanups {
		fn()
	}
	// Kill the process tree, then close the PTY.
	_ = s.process.Kill()
	_ = s.process.Wait()
	return s.closer.Close()
}

// buildEnv constructs an environment for the PTY process by starting with
// the current process environment, removing vars in unsetEnv, and appending
// extraEnv entries (KEY=val format).
func buildEnv(unsetEnv []string, extraEnv []string) []string {
	env := filterEnv(os.Environ(), unsetEnv...)
	return append(env, extraEnv...)
}
