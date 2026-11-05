// SPDX-License-Identifier: AGPL-3.0-or-later

// Package manifest beschreibt ein Stick-Abbild und prüft dessen Signatur (F-STICK-04, PLAN.md 3.12).
//
// Ein Manifest ist eine JSON-Datei (manifest.json) mit einer getrennten Signatur (manifest.json.sig,
// Ed25519 über die Bytes der Datei, base64). Der Stick-Ersteller kennt die öffentlichen Schlüssel und
// schreibt nur Abbilder, deren Größe und SHA-256 zum Manifest passen.
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

// Schema ist die einzige bekannte Fassung.
const Schema = 1

// Kompressionen der Download-Datei. Die Standardbibliothek kann nur gzip entpacken (D-01).
const (
	None = "none"
	Gzip = "gzip"
)

// MaxImage begrenzt die Größe eines Abbilds (Schutz vor unsinnigen Angaben).
const MaxImage = 64 << 30

// Manifest beschreibt ein Abbild.
type Manifest struct {
	Schema    int       `json:"schema"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Created   string    `json:"created,omitempty"`
	File      File      `json:"file"`
	Image     Image     `json:"image"`
	StickJSON StickJSON `json:"stick_json"`
}

// File ist die Datei, die heruntergeladen wird (eventuell komprimiert).
type File struct {
	Name        string `json:"name"`
	URL         string `json:"url,omitempty"` // relativ zum Manifest oder absolut (https)
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Compression string `json:"compression"`
}

// Image ist das entpackte Abbild, so wie es auf den Stick kommt (vor dem Eintragen von stick.json).
type Image struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// StickJSON ist die Lage der reservierten Datei vr/stick.json im Abbild (3.9.1).
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

// Validate prüft alle Angaben.
func (m *Manifest) Validate() error {
	switch {
	case m.Schema != Schema:
		return fmt.Errorf("unbekanntes Schema %d", m.Schema)
	case !nameRe.MatchString(m.Name):
		return errors.New("name ungültig")
	case !versionRe.MatchString(m.Version):
		return errors.New("version ungültig")
	case !fileRe.MatchString(m.File.Name):
		return errors.New("file.name ungültig")
	case m.File.Size <= 0 || m.File.Size > MaxImage:
		return errors.New("file.size ungültig")
	case !shaRe.MatchString(m.File.SHA256):
		return errors.New("file.sha256 ungültig")
	case m.File.Compression != None && m.File.Compression != Gzip:
		return errors.New("file.compression ungültig")
	case m.Image.Size <= 0 || m.Image.Size > MaxImage || m.Image.Size%512 != 0:
		return errors.New("image.size ungültig (Vielfaches von 512)")
	case !shaRe.MatchString(m.Image.SHA256):
		return errors.New("image.sha256 ungültig")
	case m.File.Compression == None && (m.File.Size != m.Image.Size || m.File.SHA256 != m.Image.SHA256):
		return errors.New("unkomprimiert: file und image müssen gleich sein")
	case m.StickJSON.Size != stick.Size:
		return fmt.Errorf("stick_json.size muss %d sein", stick.Size)
	case m.StickJSON.Offset < 1<<20 || m.StickJSON.Offset%512 != 0 || m.StickJSON.Offset+m.StickJSON.Size > m.Image.Size:
		// Das erste MiB (Partitionstabelle) bleibt unberührt.
		return errors.New("stick_json.offset ungültig")
	}
	if m.File.URL != "" {
		u, err := url.Parse(m.File.URL)
		if err != nil || (u.IsAbs() && u.Scheme != "https") || u.User != nil || strings.Contains(m.File.URL, "..") {
			return errors.New("file.url ungültig")
		}
	}
	return nil
}

// FileURL liefert die Adresse der Download-Datei, aufgelöst gegen die Adresse des Manifests.
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
		return "", errors.New("Abbild nur über HTTPS")
	}
	return u.String(), nil
}

func isLoopback(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }

// ErrSignature: Die Signatur fehlt, ist kaputt oder stammt nicht von einem bekannten Schlüssel.
var ErrSignature = errors.New("Signatur passt zu keinem bekannten Schlüssel")

// PublicKey ist ein vertrauenswürdiger Schlüssel (base64 der 32 Bytes).
type PublicKey string

// Verify prüft die Signatur von data mit einem der Schlüssel und liest das Manifest.
func Verify(data, sig []byte, keys []PublicKey) (*Manifest, error) {
	s, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(s) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: unlesbar", ErrSignature)
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

// Parse liest ein Manifest ohne Signaturprüfung (nur für Werkzeuge und Tests).
func Parse(data []byte) (*Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("Manifest unlesbar: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("Manifest: %w", err)
	}
	return &m, nil
}

// Encode schreibt ein Manifest (eingerückt, mit Zeilenende).
func (m *Manifest) Encode() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	return append(b, '\n'), err
}

// Sign signiert data mit dem privaten Schlüssel (Seed, 32 Bytes) und liefert die Signatur-Datei.
func Sign(data, seed []byte) ([]byte, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("Schlüssel ungültig")
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
	return []byte(base64.StdEncoding.EncodeToString(sig) + "\n"), nil
}

// Public liefert den öffentlichen Schlüssel zu einem Seed.
func Public(seed []byte) PublicKey {
	pk := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	return PublicKey(base64.StdEncoding.EncodeToString(pk))
}

// HexSum ist eine Hilfe für Prüfsummen.
func HexSum(sum []byte) string { return hex.EncodeToString(sum) }
