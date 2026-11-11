// Package secrets seals small values with a key kept in a local keyring file.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	keySize    = 32
	version    = 0x01
	nonceSize  = 12
	fileMode   = 0o600
	dirMode    = 0o700
	fileSuffix = "\n"
)

// Keyring holds the keys used to seal stored secrets.
type Keyring struct {
	key [keySize]byte
}

// LoadOrCreate reads the keyring at path, creating a random one if it does
// not exist. A keyring file that other users can read is refused.
func LoadOrCreate(path string) (*Keyring, error) {
	key, err := readKey(path)
	if err == nil {
		k := &Keyring{}
		copy(k.key[:], key)
		return k, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return create(path)
}

func readKey(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read keyring: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read keyring %s: not a regular file", path)
	}
	if info.Mode().Perm()&0o044 != 0 {
		return nil, fmt.Errorf("keyring %s is readable by group or others; run chmod 600 %s", path, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read keyring: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(data)))
	}
	if err != nil {
		return nil, fmt.Errorf("read keyring %s: %w", path, err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("keyring %s holds %d bytes, want %d", path, len(key), keySize)
	}
	return key, nil
}

func create(path string) (*Keyring, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("create keyring directory: %w", err)
	}
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate keyring: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(key) + fileSuffix
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if errors.Is(err, os.ErrExist) {
		// Another process won the race; use its key.
		return LoadOrCreate(path)
	}
	if err != nil {
		return nil, fmt.Errorf("create keyring: %w", err)
	}
	if _, err := f.WriteString(encoded); err != nil {
		f.Close()
		return nil, fmt.Errorf("write keyring: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("write keyring: %w", err)
	}
	k := &Keyring{}
	copy(k.key[:], key)
	return k, nil
}

// Seal encrypts plaintext with AES-256-GCM. The output is a version byte, a
// random 12-byte nonce and the ciphertext.
func (k *Keyring) Seal(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(k.key[:])
	if err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}
	out := make([]byte, 1+nonceSize, 1+nonceSize+len(plaintext)+gcm.Overhead())
	out[0] = version
	if _, err := rand.Read(out[1:]); err != nil {
		return nil, fmt.Errorf("seal: read nonce: %w", err)
	}
	out = gcm.Seal(out, out[1:1+nonceSize], plaintext, nil)
	return out, nil
}

// Open decrypts a value produced by Seal.
func (k *Keyring) Open(sealed []byte) ([]byte, error) {
	if len(sealed) < 1+nonceSize {
		return nil, errors.New("open: sealed value is too short")
	}
	if sealed[0] != version {
		return nil, fmt.Errorf("open: unknown sealed version %d", sealed[0])
	}
	block, err := aes.NewCipher(k.key[:])
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	plaintext, err := gcm.Open(nil, sealed[1:1+nonceSize], sealed[1+nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return plaintext, nil
}
