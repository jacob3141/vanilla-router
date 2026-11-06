// SPDX-License-Identifier: AGPL-3.0-or-later

// Package manifest describes a stick image and verifies its signature (F-STICK-04, PLAN.md 3.12).
//
// A manifest is a JSON file (manifest.json) with a separate signature (manifest.json.sig,
// Ed25519 over the file's bytes, base64). The stick creator knows the public keys and
// only writes images whose size and SHA-256 match the manifest.
package manifest

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"vanilla-router/internal/stick"
)

// Schema is the only known version.
const Schema = 1

// Compressions of the download file. The standard library can only decompress gzip (D-01).
const (
	None = "none"
	Gzip = "gzip"
)

// MaxImage limits the size of an image (protection against nonsensical values).
const MaxImage = 64 << 30

// Manifest describes an image.
type Manifest struct {
	Schema    int       `json:"schema"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Created   string    `json:"created,omitempty"`
	File      File      `json:"file"`
	Image     Image     `json:"image"`
	StickJSON StickJSON `json:"stick_json"`
}

// File is the file that is downloaded (possibly compressed).
type File struct {
	Name        string `json:"name"`
	URL         string `json:"url,omitempty"` // relative to the manifest or absolute (https)
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Compression string `json:"compression"`
}

// Image is the unpacked image as it goes on the stick (before stick.json is added).
type Image struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// StickJSON is the location of the reserved file vr/stick.json in the image (3.9.1).
type StickJSON struct {
	Offset int64 `json:"offset"`
	Size   int64 `json:"size"`
}

var (
	nameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	versionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+~_-]{0,63}$`)
	fileRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~-]{0,127}$`)
	shaRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Validate checks all values.
func (m *Manifest) Validate() error {
	switch {
	case m.Schema != Schema:
		return fmt.Errorf("unknown schema %d", m.Schema)
	case !nameRe.MatchString(m.Name):
		return errors.New("name invalid")
	case !versionRe.MatchString(m.Version):
		return errors.New("version invalid")
	case !fileRe.MatchString(m.File.Name):
		return errors.New("file.name invalid")
	case m.File.Size <= 0 || m.File.Size > MaxImage:
		return errors.New("file.size invalid")
	case !shaRe.MatchString(m.File.SHA256):
		return errors.New("file.sha256 invalid")
	case m.File.Compression != None && m.File.Compression != Gzip:
		return errors.New("file.compression invalid")
	case m.Image.Size <= 0 || m.Image.Size > MaxImage || m.Image.Size%512 != 0:
		return errors.New("image.size invalid (multiple of 512)")
	case !shaRe.MatchString(m.Image.SHA256):
		return errors.New("image.sha256 invalid")
	case m.File.Compression == None && (m.File.Size != m.Image.Size || m.File.SHA256 != m.Image.SHA256):
		return errors.New("uncompressed: file and image must be equal")
	case m.StickJSON.Size != stick.Size:
		return fmt.Errorf("stick_json.size must be %d", stick.Size)
	case m.StickJSON.Offset < 1<<20 || m.StickJSON.Offset%512 != 0 || m.StickJSON.Offset+m.StickJSON.Size > m.Image.Size:
		// The first MiB (partition table) stays untouched.
		return errors.New("stick_json.offset invalid")
	}
	if m.File.URL != "" {
		u, err := url.Parse(m.File.URL)
		if err != nil || (u.IsAbs() && u.Scheme != "https") || u.User != nil || strings.Contains(m.File.URL, "..") {
			return errors.New("file.url invalid")
		}
	}
	return nil
}

// FileURL returns the URL of the download file, resolved against the manifest's URL.
func (m *Manifest) FileURL(manifestURL string) (string, error) {
	base, err := url.Parse(manifestURL)
	if err != nil {
		return "", err
	}
	ref := m.File.URL
	if ref == "" {
		ref = m.File.Name
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	u := base.ResolveReference(r)
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return "", errors.New("image over HTTPS only")
	}
	return u.String(), nil
}

func isLoopback(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }

// ErrSignature: the signature is missing, broken or not from a known key.
var ErrSignature = errors.New("signature matches no known key")

// PublicKey is a trusted key (base64 of the 32 bytes).
type PublicKey string

// Verify checks the signature of data with one of the keys and reads the manifest.
func Verify(data, sig []byte, keys []PublicKey) (*Manifest, error) {
	s, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(s) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: unreadable", ErrSignature)
	}
	ok := false
	for _, k := range keys {
		pk, err := base64.StdEncoding.DecodeString(string(k))
		if err != nil || len(pk) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(ed25519.PublicKey(pk), data, s) {
			ok = true
			break
		}
	}
	if !ok {
		return nil, ErrSignature
	}
	return Parse(data)
}

// Parse reads a manifest without checking the signature (for tools and tests only).
func Parse(data []byte) (*Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest unreadable: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("Manifest: %w", err)
	}
	return &m, nil
}

// Encode writes a manifest (indented, with a trailing newline).
func (m *Manifest) Encode() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	return append(b, '\n'), err
}

// Sign signs data with the private key (seed, 32 bytes) and returns the signature file.
func Sign(data, seed []byte) ([]byte, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("key invalid")
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
	return []byte(base64.StdEncoding.EncodeToString(sig) + "\n"), nil
}

// Public returns the public key for a seed.
func Public(seed []byte) PublicKey {
	pk := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	return PublicKey(base64.StdEncoding.EncodeToString(pk))
}

// HexSum is a helper for checksums.
func HexSum(sum []byte) string { return hex.EncodeToString(sum) }
