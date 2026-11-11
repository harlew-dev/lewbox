package service

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	passwordMinLength = 12
	passwordMaxLength = 256

	// The hash string records its own iteration count, so it can be raised
	// later without breaking stored hashes.
	passwordSaltLength = 16
	passwordKeyLength  = 32
	passwordPrefix     = "pbkdf2-sha256"

	sessionTokenPrefix = "lbs_"
	sessionTokenBytes  = 32
	sessionLifetime    = 30 * 24 * time.Hour

	// Browser sessions are shorter than CLI tokens and end after a quiet
	// spell even when the absolute lifetime has not run out.
	webSessionLifetime = 7 * 24 * time.Hour
	webSessionIdle     = 12 * time.Hour
)

// passwordIterations is a variable so tests can lower it.
var passwordIterations = 600000

var errBadPasswordHash = errors.New("malformed password hash")

// validatePassword enforces the length limits and reports an invalid request.
func validatePassword(password string) error {
	if len(password) < passwordMinLength {
		return invalid("password must be at least %d characters", passwordMinLength)
	}
	if len(password) > passwordMaxLength {
		return invalid("password must be at most %d characters", passwordMaxLength)
	}
	return nil
}

// hashPassword returns a self-describing PBKDF2-HMAC-SHA256 hash.
func hashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyLength)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return fmt.Sprintf("%s$%d$%s$%s",
		passwordPrefix,
		passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// verifyPassword checks password against an encoded hash, using the iteration
// count stored alongside it.
func verifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != passwordPrefix {
		return false, errBadPasswordHash
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false, errBadPasswordHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false, errBadPasswordHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false, errBadPasswordHash
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false, fmt.Errorf("hash password: %w", err)
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// newSessionToken returns a bearer token and the SHA-256 hex digest that is
// stored in the database.
func newSessionToken() (token, digest string, err error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("read token: %w", err)
	}
	token = sessionTokenPrefix + hex.EncodeToString(b)
	return token, tokenDigest(token), nil
}

// tokenDigest is the hex SHA-256 of a bearer token.
func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// validSessionToken reports whether token has the exact shape newSessionToken
// produces: lbs_ followed by 64 lowercase hex characters.
func validSessionToken(token string) bool {
	rest, ok := strings.CutPrefix(token, sessionTokenPrefix)
	if !ok || len(rest) != sessionTokenBytes*2 {
		return false
	}
	for _, c := range rest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
