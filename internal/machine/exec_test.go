package machine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestExecRunnerSudoArgv runs the real ExecRunner against a fake `sudo` on
// PATH that prints its arguments. The fake runner used elsewhere can't catch
// a wrong argv, and once one did: sudo got the arguments without the
// command name.
func TestExecRunnerSudoArgv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script on PATH")
	}
	bin := t.TempDir()
	fake := "#!/bin/sh\nprintf '%s\\n' \"$*\"\n"
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	tests := []struct {
		name     string
		terminal bool
		cmd      Cmd
		want     string
	}{
		{
			name: "no terminal adds -n",
			cmd:  Cmd{Name: "bash", Args: []string{"/c/install.sh", "--version", "v0.7.0"}, Sudo: true},
			want: "-n bash /c/install.sh --version v0.7.0",
		},
		{
			name:     "terminal lets sudo prompt",
			terminal: true,
			cmd:      Cmd{Name: "systemctl", Args: []string{"restart", "sandboxd"}, Sudo: true},
			want:     "systemctl restart sandboxd",
		},
		{
			name: "secrets are masked in streamed output",
			cmd:  Cmd{Name: "bash", Args: []string{"x", "--token", "s3cret"}, Sudo: true, Secrets: []string{"s3cret"}},
			want: "-n bash x --token " + Mask,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			r := &ExecRunner{Stdout: &out, Stderr: &out, StdinTerminal: tt.terminal}
			if err := r.Run(context.Background(), tt.cmd); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(out.String()); got != tt.want {
				t.Errorf("sudo got %q, want %q", got, tt.want)
			}
			b, err := r.Output(context.Background(), Cmd{Name: tt.cmd.Name, Args: tt.cmd.Args, Sudo: true})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), tt.cmd.Name+" ") {
				t.Errorf("Output: sudo got %q, want the command name %q", b, tt.cmd.Name)
			}
		})
	}
}
