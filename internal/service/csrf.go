package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"

	"github.com/harlew-dev/lewbox/internal/secrets"
)

// newCSRFKey derives the key behind CSRF tokens from the keyring, so tokens
// survive a restart. Without a keyring (some tests and tools) it is random
// for the life of the process.
func newCSRFKey(k *secrets.Keyring) []byte {
	if k != nil {
		if key, err := k.DeriveKey("csrf v1"); err == nil {
			return key
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("csrf key: " + err.Error())
	}
	return key
}

// CSRFToken is the token a browser session sends with every write: an HMAC
// of the raw session token. Nothing is stored, it rotates with the session,
// and a database copy (which holds only session digests) cannot produce it.
func (s *Service) CSRFToken(sessionToken string) string {
	mac := hmac.New(sha256.New, s.csrfKey)
	mac.Write([]byte(sessionToken))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ValidCSRFToken reports, in constant time, whether token belongs to the
// session.
func (s *Service) ValidCSRFToken(sessionToken, token string) bool {
	if token == "" {
		return false
	}
	want := s.CSRFToken(sessionToken)
	return subtle.ConstantTimeCompare([]byte(want), []byte(token)) == 1
}
