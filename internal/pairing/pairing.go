// SPDX-License-Identifier: AGPL-3.0-or-later

// Package pairing holds what router and access point both compute when an AP is paired (docs/sdn-concept.md 4.2,
// 4.3): the identities as Ed25519 keys and the comparison code the owner reads on both screens.
//
// The code is a short authentication string over both public keys and two random values. The AP commits to its
// value (hash) before it learns the router's value, so somebody in between cannot try keys until six digits
// match: a forged key changes the code, and with the commitment there is exactly one try per pairing.
//
//	AP → router:  ap_key, commitment = H(ap_nonce)
//	router → AP:  router_key, router_nonce
//	AP → router:  ap_nonce              (router checks the commitment)
//	both:         code = H(router_key, ap_key, router_nonce, ap_nonce) → "418 207"
//
// The transport (vr-apd, TLS with pinned keys) does not exist yet. This package only fixes the computation so that
// both sides and the web interface agree on it.
package pairing

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// NonceSize is the length of both random values.
const NonceSize = 32

const (
	commitLabel = "vanilla-router pairing commit v1"
	codeLabel   = "vanilla-router pairing code v1"
	keyPrefix   = "ed25519:"
)

// NewKey creates an identity. The private key never leaves the computer that made it.
func NewKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// FormatKey writes a public key as in config.json: ed25519:<base64>.
func FormatKey(pub ed25519.PublicKey) string {
	return keyPrefix + base64.StdEncoding.EncodeToString(pub)
}

// ParseKey reads a public key in the form of FormatKey.
func ParseKey(s string) (ed25519.PublicKey, error) {
	b64, ok := strings.CutPrefix(s, keyPrefix)
	if !ok {
		return nil, errors.New("not an ed25519 key")
	}
	k, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(k) != ed25519.PublicKeySize {
		return nil, errors.New("invalid ed25519 key")
	}
	return ed25519.PublicKey(k), nil
}

// Fingerprint is a short form of a key for display, e.g. in the list of access points.
func Fingerprint(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	s := hex.EncodeToString(h[:6])
	return s[0:4] + " " + s[4:8] + " " + s[8:12]
}

// Commit is the AP's commitment to its random value.
func Commit(apNonce []byte) []byte {
	h := sha256.New()
	h.Write([]byte(commitLabel))
	h.Write(apNonce)
	return h.Sum(nil)
}

// CheckCommit reports whether the revealed value matches the commitment.
func CheckCommit(commitment, apNonce []byte) bool {
	return len(apNonce) == NonceSize && subtle.ConstantTimeCompare(commitment, Commit(apNonce)) == 1
}

// Code computes the comparison code, six digits in two groups.
func Code(routerKey, apKey ed25519.PublicKey, routerNonce, apNonce []byte) (string, error) {
	if len(routerKey) != ed25519.PublicKeySize || len(apKey) != ed25519.PublicKeySize {
		return "", errors.New("invalid key")
	}
	if len(routerNonce) != NonceSize || len(apNonce) != NonceSize {
		return "", errors.New("invalid random value")
	}
	h := sha256.New()
	h.Write([]byte(codeLabel))
	for _, p := range [][]byte{routerKey, apKey, routerNonce, apNonce} {
		// Length prefixes, so that no two different inputs give the same byte string
		binary.Write(h, binary.BigEndian, uint16(len(p)))
		h.Write(p)
	}
	n := binary.BigEndian.Uint64(h.Sum(nil)[:8]) % 1_000_000
	return fmt.Sprintf("%03d %03d", n/1000, n%1000), nil
}

// Nonce returns a new random value.
func Nonce() ([]byte, error) {
	b := make([]byte, NonceSize)
	_, err := rand.Read(b)
	return b, err
}
