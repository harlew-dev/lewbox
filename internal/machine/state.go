package machine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	// ModeLocal is a development install on 127.0.0.1 with no cluster.
	ModeLocal = "local"
	// ModeDomain is a production install with TLS for one domain.
	ModeDomain = "domain"
	// ModePublicHost is an install reachable at a public host or IP without TLS.
	ModePublicHost = "public-host"

	stateFile = "node.json"
	dirMode   = 0o700
	fileMode  = 0o600
)

// State records what this machine installed, so node add and update know what
// to do here. It holds no secrets.
type State struct {
	Name           string    `json:"name"`
	Mode           string    `json:"mode"`
	Domain         string    `json:"domain,omitempty"`
	PublicHost     string    `json:"public_host,omitempty"`
	Runtime        string    `json:"runtime"`
	AerolVMVersion string    `json:"aerolvm_version"`
	From           string    `json:"from,omitempty"`
	InstalledAt    time.Time `json:"installed_at"`
	Cluster        string    `json:"cluster,omitempty"`
	StoredAPIURL   string    `json:"api_url,omitempty"`
}

// APIURL is the URL at which this machine's AerolVM answers.
func (s State) APIURL() string {
	switch s.Mode {
	case ModeDomain:
		if s.Domain != "" {
			return "https://" + s.Domain
		}
	case ModePublicHost:
		if s.PublicHost != "" {
			return "http://" + s.PublicHost + ":21212"
		}
	}
	return "http://127.0.0.1:21212"
}

// Dir is $XDG_CONFIG_HOME/lewbox, falling back to ~/.config/lewbox.
func Dir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "lewbox"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "lewbox"), nil
}

// Path is the file Load and Save use.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, stateFile), nil
}

// Load reads this machine's State. A missing file returns an error that
// matches fs.ErrNotExist, which means AerolVM was not installed by Lewbox.
func Load() (State, error) {
	path, err := Path()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// IsNotInstalled reports whether err means there is no saved State.
func IsNotInstalled(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// Save writes State atomically with 0600 permissions.
func (s State) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return writeAtomic(path, append(data, '\n'))
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(fileMode); err != nil {
		f.Close()
		return fmt.Errorf("set state permissions: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync state: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
