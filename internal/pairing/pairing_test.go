// SPDX-License-Identifier: AGPL-3.0-or-later

package pairing

import (
	"bytes"
	"crypto/ed25519"
	"regexp"
	"testing"
)

func key(seed byte) ed25519.PublicKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, 32)).Public().(ed25519.PublicKey)
}

func TestCode(t *testing.T) {
	r, a := key(1), key(2)
	nr, na := bytes.Repeat([]byte{3}, NonceSize), bytes.Repeat([]byte{4}, NonceSize)
	c1, err := Code(r, a, nr, na)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9]{3} [0-9]{3}$`).MatchString(c1) {
		t.Fatalf("format: %q", c1)
	}
	// Fixed value: router and AP must compute the same code in every version
	if c1 != "097 598" {
		t.Errorf("code changed: %q", c1)
	}
	for name, args := range map[string][4][]byte{
		"other AP key":     {r, key(9), nr, na},
		"other router key": {key(9), a, nr, na},
		"keys swapped":     {a, r, nr, na},
		"other AP nonce":   {r, a, nr, bytes.Repeat([]byte{5}, NonceSize)},
	} {
		c, err := Code(args[0], args[1], args[2], args[3])
		if err != nil || c == c1 {
			t.Errorf("%s: %q %v", name, c, err)
		}
	}
	if _, err := Code(r, a, nr[:5], na); err == nil {
		t.Error("short nonce accepted")
	}
}

func TestCommit(t *testing.T) {
	n, _ := Nonce()
	c := Commit(n)
	if !CheckCommit(c, n) {
		t.Fatal("own commitment refused")
	}
	m := bytes.Clone(n)
	m[0] ^= 1
	if CheckCommit(c, m) || CheckCommit(c, n[:16]) {
		t.Error("wrong value accepted")
	}
}

func TestKeyFormat(t *testing.T) {
	k := key(7)
	s := FormatKey(k)
	back, err := ParseKey(s)
	if err != nil || !bytes.Equal(back, k) {
		t.Fatal(s, err)
	}
	for _, bad := range []string{"", "ed25519:", "rsa:AAAA", "ed25519:AAEC", s + "x"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if f := Fingerprint(k); len(f) != 14 {
		t.Errorf("fingerprint %q", f)
	}
}
