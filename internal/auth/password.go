// SPDX-License-Identifier: AGPL-3.0-or-later

// Package auth enthält Passwort-Hashing, Sitzungen und die Login-Bremse der Weboberfläche.
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

// OWASP-Empfehlung für PBKDF2-HMAC-SHA256 (Stand 2025)
const pbkdf2Iterations = 600_000

// MinPasswordLength ist die Mindestlänge für das Passwort der Oberfläche.
const MinPasswordLength = 12

var b64 = base64.RawStdEncoding

// ValidatePassword prüft die Passwortregeln und liefert eine Meldung für die Oberfläche.
func ValidatePassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLength {
		return fmt.Errorf("Das Passwort muss mindestens %d Zeichen lang sein.", MinPasswordLength)
	}
	if len(pw) > 1024 {
		return errors.New("Das Passwort ist zu lang.")
	}
	return nil
}

// HashPassword erzeugt "pbkdf2-sha256$<runden>$<salt>$<hash>".
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	rand.Read(salt)
	dk, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations, b64.EncodeToString(salt), b64.EncodeToString(dk)), nil
}

// VerifyPassword vergleicht in konstanter Zeit.
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
