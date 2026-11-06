// SPDX-License-Identifier: AGPL-3.0-or-later

package manifest

import (
	"bytes"
	"strings"
	"testing"

	"vanilla-router/internal/stick"
)

func sample() *Manifest {
	return &Manifest{Schema: 1, Name: "vanilla-router-stick", Version: "0.1.0",
		File:      File{Name: "vanilla-router-stick-0.1.0.img.gz", Size: 1000, SHA256: strings.Repeat("a", 64), Compression: Gzip},
		Image:     Image{Size: 4 << 20, SHA256: strings.Repeat("b", 64)},
		StickJSON: StickJSON{Offset: 2 << 20, Size: stick.Size}}
}

func TestSignAndVerify(t *testing.T) {
	seed := bytes.Repeat([]byte{7}, 32)
	other := bytes.Repeat([]byte{8}, 32)
	data, err := sample().Encode()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := Sign(data, seed)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Verify(data, sig, []PublicKey{Public(other), Public(seed)})
	if err != nil || m.Version != "0.1.0" {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := Verify(data, sig, []PublicKey{Public(other)}); err == nil {
		t.Error("foreign key must fail")
	}
	bad := bytes.Replace(data, []byte("0.1.0"), []byte("0.1.1"), 1)
	if _, err := Verify(bad, sig, []PublicKey{Public(seed)}); err == nil {
		t.Error("changed data must fail")
	}
	if _, err := Verify(data, []byte("broken"), []PublicKey{Public(seed)}); err == nil {
		t.Error("broken signature")
	}
	// Signed, but invalid in content: is rejected anyway.
	inv := []byte(strings.Replace(string(data), `"schema": 1`, `"schema": 2`, 1))
	sig2, _ := Sign(inv, seed)
	if _, err := Verify(inv, sig2, []PublicKey{Public(seed)}); err == nil {
		t.Error("invalid manifest")
	}
}

func TestValidate(t *testing.T) {
	for name, mod := range map[string]func(*Manifest){
		"name":             func(m *Manifest) { m.Name = "../x" },
		"file":             func(m *Manifest) { m.File.Name = "a/b" },
		"sha":              func(m *Manifest) { m.File.SHA256 = "XYZ" },
		"compression":      func(m *Manifest) { m.File.Compression = "zstd" },
		"size":             func(m *Manifest) { m.Image.Size = 4<<20 + 1 },
		"offset front":     func(m *Manifest) { m.StickJSON.Offset = 4096 },
		"offset back":      func(m *Manifest) { m.StickJSON.Offset = 4 << 20 },
		"offset unaligned": func(m *Manifest) { m.StickJSON.Offset = 2<<20 + 1 },
		"stick size":       func(m *Manifest) { m.StickJSON.Size = 512 },
		"http":             func(m *Manifest) { m.File.URL = "http://example.org/x.img" },
		"path":             func(m *Manifest) { m.File.URL = "../secret" },
		"uncompressed":     func(m *Manifest) { m.File.Compression = None },
		"huge":             func(m *Manifest) { m.Image.Size = MaxImage + 512 },
		"credentials":      func(m *Manifest) { m.File.URL = "https://u:p@example.org/x" },
	} {
		m := sample()
		mod(m)
		if m.Validate() == nil {
			t.Errorf("%s: must be invalid", name)
		}
	}
	if err := sample().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownFields(t *testing.T) {
	data, _ := sample().Encode()
	data = bytes.Replace(data, []byte(`"schema": 1,`), []byte(`"schema": 1, "extra": true,`), 1)
	if _, err := Parse(data); err == nil {
		t.Error("unknown fields must be rejected")
	}
}

func TestFileURL(t *testing.T) {
	m := sample()
	u, err := m.FileURL("https://example.org/stick/manifest.json")
	if err != nil || u != "https://example.org/stick/vanilla-router-stick-0.1.0.img.gz" {
		t.Fatalf("%s %v", u, err)
	}
	m.File.URL = "https://downloads.example.org/a.img.gz"
	if u, _ := m.FileURL("https://example.org/m.json"); u != m.File.URL {
		t.Error(u)
	}
	m.File.URL = ""
	if _, err := m.FileURL("http://example.org/m.json"); err == nil {
		t.Error("HTTP must fail")
	}
	if _, err := m.FileURL("http://127.0.0.1:8080/m.json"); err != nil {
		t.Error("local test server over HTTP allowed")
	}
}
