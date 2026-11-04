// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ensureCert legt ein selbstsigniertes ECDSA-Zertifikat für name und ip an (Q-09: selbstsigniert mit
// Anleitung). Ein vorhandenes bleibt, solange es noch mindestens 30 Tage gilt und Name und Adresse
// passen. Der Schlüssel gehört root und ist für die Gruppe gid lesbar (0640).
func ensureCert(dir, name string, ip netip.Addr, gid int) (bool, error) {
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if b, err := os.ReadFile(certPath); err == nil {
		if blk, _ := pem.Decode(b); blk != nil {
			if c, err := x509.ParseCertificate(blk.Bytes); err == nil && certOK(c, name, ip) {
				if _, err := os.Stat(keyPath); err == nil {
					return false, nil
				}
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return false, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name, Organization: []string{"vanilla-router"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(3, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{name},
		IPAddresses:           []net.IP{net.IP(ip.AsSlice())},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return false, err
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return false, err
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", kder, 0o640, gid); err != nil {
		return false, err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0o644, -1); err != nil {
		return false, err
	}
	return true, nil
}

func certOK(c *x509.Certificate, name string, ip netip.Addr) bool {
	if time.Until(c.NotAfter) < 30*24*time.Hour || !slices.Contains(c.DNSNames, name) {
		return false
	}
	for _, a := range c.IPAddresses {
		if a.Equal(net.IP(ip.AsSlice())) {
			return true
		}
	}
	return false
}

func writePEM(path, typ string, der []byte, mode os.FileMode, gid int) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if err := pem.Encode(f, &pem.Block{Type: typ, Bytes: der}); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if gid >= 0 {
		if err := os.Chown(tmp, 0, gid); err != nil && !errors.Is(err, os.ErrPermission) {
			return err
		}
	}
	os.Chmod(tmp, mode)
	return os.Rename(tmp, path)
}
