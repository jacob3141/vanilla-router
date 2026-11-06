// SPDX-License-Identifier: AGPL-3.0-or-later

// Package auth contains password hashing, sessions and the login throttle of the web interface.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// OWASP recommendation for PBKDF2-HMAC-SHA256 (as of 2025)
const pbkdf2Iterations = 600_000

// MinPasswordLength is the minimum length of the web interface password.
const MinPasswordLength = 12

var b64 = base64.RawStdEncoding

// ValidatePassword checks the password rules and returns a message for the web interface.
func ValidatePassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLength {
		return fmt.Errorf("The password must be at least %d characters long.", MinPasswordLength)
	}
	if len(pw) > 1024 {
		return errors.New("The password is too long.")
	}
	return nil
}

// HashPassword creates "pbkdf2-sha256$<rounds>$<salt>$<hash>".
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	rand.Read(salt)
	dk, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations, b64.EncodeToString(salt), b64.EncodeToString(dk)), nil
}

// VerifyPassword compares in constant time.
func VerifyPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 100_000 {
		return false
	}
	salt, err1 := b64.DecodeString(parts[2])
	want, err2 := b64.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
