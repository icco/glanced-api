// Package auth hashes passwords and manages bearer-token sessions.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

var b64 = base64.RawStdEncoding

// HashPassword returns an argon2id PHC string for pw.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword reports whether pw matches the PHC string encoded.
func VerifyPassword(encoded, pw string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, fmt.Errorf("unsupported argon2 version %q", parts[2])
	}
	var mem, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false, fmt.Errorf("parse params: %w", err)
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode salt: %w", err)
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decode hash: %w", err)
	}
	got := argon2.IDKey([]byte(pw), salt, t, mem, p, uint32(len(want))) //nolint:gosec // hash length is 32
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is verified against for unknown emails, so login takes as long
// whether or not the account exists.
var dummyHash = sync.OnceValue(func() string {
	h, err := HashPassword("glanced dummy password")
	if err != nil {
		panic(fmt.Sprintf("hash dummy password: %v", err))
	}
	return h
})
