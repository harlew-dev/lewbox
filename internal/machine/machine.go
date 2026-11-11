// Package machine runs the privileged steps that change the machine the CLI is
// on: installing AerolVM, starting or joining a cluster, and updating both.
//
// Every privileged command is printed with its secrets masked before it runs,
// and output that the installer streams is masked line by line.
package machine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Mask replaces a secret in printed output.
const Mask = "******"

// Cmd is one command to run. Secrets are replaced in its display and in
// streamed output.
type Cmd struct {
	Name    string
	Args    []string
	Sudo    bool
	Stdin   io.Reader
	Secrets []string
}

// Display returns the command line, prefixed with sudo and with secrets
// masked, so it can be printed before it runs.
func (c Cmd) Display() string {
	parts := make([]string, 0, len(c.Args)+2)
	if c.Sudo {
		parts = append(parts, "sudo")
	}
	parts = append(parts, c.Name)
	for _, arg := range c.Args {
		parts = append(parts, quoteArg(arg))
	}
	return MaskLine(strings.Join(parts, " "), c.Secrets)
}

// Runner runs commands, either for real or in a test fake.
type Runner interface {
	// Run runs cmd and streams its output through the masker.
	Run(ctx context.Context, cmd Cmd) error
	// Output runs cmd and returns its stdout.
	Output(ctx context.Context, cmd Cmd) ([]byte, error)
}

// ExecRunner runs commands with os/exec. Output is written to Stdout and
// Stderr, masked.
type ExecRunner struct {
	Stdout io.Writer
	Stderr io.Writer
	// Stdin is used when a Cmd has none of its own.
	Stdin io.Reader
	// StdinTerminal says whether the CLI's stdin is a terminal. When it is
	// not, sudo gets -n so it fails instead of waiting for a password.
	StdinTerminal bool
}

var _ Runner = (*ExecRunner)(nil)

// Run runs cmd and streams both output streams through a masker.
func (r *ExecRunner) Run(ctx context.Context, cmd Cmd) error {
	c := r.command(ctx, cmd)
	r.attachStdin(c, cmd)
	stdout := newMaskWriter(r.Stdout, cmd.Secrets)
	stderr := newMaskWriter(r.Stderr, cmd.Secrets)
	c.Stdout = stdout
	c.Stderr = stderr
	err := c.Run()
	if ferr := stdout.Flush(); err == nil {
		err = ferr
	}
	if ferr := stderr.Flush(); err == nil {
		err = ferr
	}
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Display(), err)
	}
	return nil
}

// Output runs cmd and returns its stdout. Secrets are masked in the error
// message, which may include stderr.
func (r *ExecRunner) Output(ctx context.Context, cmd Cmd) ([]byte, error) {
	c := r.command(ctx, cmd)
	r.attachStdin(c, cmd)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("%s: %w: %s", cmd.Display(), err, MaskLine(detail, cmd.Secrets))
		}
		return nil, fmt.Errorf("%s: %w", cmd.Display(), err)
	}
	return stdout.Bytes(), nil
}

func (r *ExecRunner) attachStdin(c *exec.Cmd, cmd Cmd) {
	if cmd.Stdin != nil {
		c.Stdin = cmd.Stdin
	} else if r.Stdin != nil {
		c.Stdin = r.Stdin
	}
}

func (r *ExecRunner) command(ctx context.Context, cmd Cmd) *exec.Cmd {
	name := cmd.Name
	args := cmd.Args
	if cmd.Sudo {
		sudoArgs := make([]string, 0, len(args)+2)
		if !r.StdinTerminal {
			sudoArgs = append(sudoArgs, "-n")
		}
		sudoArgs = append(sudoArgs, name)
		sudoArgs = append(sudoArgs, args...)
		name = "sudo"
		args = sudoArgs
	}
	return exec.CommandContext(ctx, name, args...)
}

// MaskLine replaces known secrets with ****** and rewrites a PAT line so the
// token it prints never reaches the terminal or a log file.
func MaskLine(line string, secrets []string) string {
	if strings.HasPrefix(line, "PAT token:") {
		return "PAT token: " + Mask
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		line = strings.ReplaceAll(line, secret, Mask)
	}
	return line
}

// maskWriter masks output line by line. A line is only written once its
// newline has arrived, so a secret split across writes is still masked.
type maskWriter struct {
	w       io.Writer
	secrets []string
	buf     []byte
}

func newMaskWriter(w io.Writer, secrets []string) *maskWriter {
	if w == nil {
		w = io.Discard
	}
	return &maskWriter{w: w, secrets: secrets}
}

func (m *maskWriter) Write(p []byte) (int, error) {
	m.buf = append(m.buf, p...)
	for {
		i := bytes.IndexByte(m.buf, '\n')
		if i < 0 {
			break
		}
		if err := m.writeLine(m.buf[:i]); err != nil {
			return 0, err
		}
		m.buf = m.buf[i+1:]
	}
	return len(p), nil
}

// Flush writes a last line that had no newline.
func (m *maskWriter) Flush() error {
	if len(m.buf) == 0 {
		return nil
	}
	err := m.writeLine(m.buf)
	m.buf = nil
	return err
}

func (m *maskWriter) writeLine(line []byte) error {
	_, err := fmt.Fprintln(m.w, MaskLine(string(line), m.secrets))
	return err
}

// quoteArg quotes an argument for printing only; it is never used to build
// the argument list passed to a process.
func quoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	for _, r := range arg {
		if strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:=@%+,-", r) {
			continue
		}
		return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return arg
}
