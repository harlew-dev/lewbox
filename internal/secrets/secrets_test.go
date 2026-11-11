package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTripAndFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "keyring.key")
	k, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("keyring mode = %o, want 600", perm)
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("keyring directory mode = %o, want 700", perm)
	}

	plaintext := []byte("avm_super_secret_token")
	sealed, err := k.Seal(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plaintext) {
		t.Error("sealed value contains the plaintext")
	}
	if sealed[0] != 0x01 {
		t.Errorf("version byte = %#x", sealed[0])
	}
	got, err := k.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("opened %q, want %q", got, plaintext)
	}

	// Reloading the same file yields the same key.
	again, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := again.Open(sealed); err != nil {
		t.Errorf("reloaded key cannot open: %v", err)
	}
}

func TestTamperAndWrongKey(t *testing.T) {
	dir := t.TempDir()
	k, err := LoadOrCreate(filepath.Join(dir, "one.key"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := k.Seal([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := k.Open(tampered); err == nil {
		t.Error("Open accepted a tampered value")
	}

	other, err := LoadOrCreate(filepath.Join(dir, "two.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(sealed); err == nil {
		t.Error("Open accepted a value sealed with another key")
	}

	for _, bad := range [][]byte{nil, {}, {0x01}, {0x02, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}} {
		if _, err := k.Open(bad); err == nil {
			t.Errorf("Open(%v) succeeded", bad)
		}
	}
}

func TestPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring.key")
	if _, err := LoadOrCreate(path); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreate(path); err == nil {
			t.Errorf("LoadOrCreate accepted mode %o", mode)
		} else if !strings.Contains(err.Error(), "readable") {
			t.Errorf("error for mode %o = %v", mode, err)
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err != nil {
		t.Errorf("LoadOrCreate with mode 600: %v", err)
	}
}
