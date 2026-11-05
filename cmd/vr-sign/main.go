// SPDX-License-Identifier: AGPL-3.0-or-later

// vr-sign erzeugt Schlüssel und signierte Manifeste für Stick-Abbilder (F-STICK-04, PLAN.md 3.12).
// Der private Schlüssel gehört auf einen Rechner ohne Netz (Q-19), nie ins Repo.
//
//	vr-sign keygen -o stick.key
//	vr-sign manifest -key stick.key -image dist/vanilla-router-stick.raw -version 0.1.0 -gzip -o dist/release
//	vr-sign verify -pub <base64> dist/release/manifest.json
package main

import (
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vanilla-router/internal/manifest"
	"vanilla-router/internal/stick"
	"vanilla-router/internal/stickwrite"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "manifest":
		err = makeManifest(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vr-sign:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Aufruf: vr-sign keygen|manifest|verify … (siehe Kopf von cmd/vr-sign/main.go)")
	os.Exit(2)
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("o", "", "Datei für den privaten Schlüssel")
	fs.Parse(args)
	if *out == "" {
		return fmt.Errorf("-o fehlt")
	}
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return err
	}
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(seed)); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(manifest.Public(seed))
	return nil
}

func readSeed(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != 32 {
		return nil, fmt.Errorf("%s: kein gültiger Schlüssel", path)
	}
	return seed, nil
}

func sumFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return n, manifest.HexSum(h.Sum(nil)), err
}

func makeManifest(args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ExitOnError)
	key := fs.String("key", "", "privater Schlüssel (keygen)")
	img := fs.String("image", "", "Abbild (roh)")
	ver := fs.String("version", "", "Version")
	name := fs.String("name", "vanilla-router-stick", "Name")
	url := fs.String("url", "", "Adresse der Datei (leer: neben dem Manifest)")
	gz := fs.Bool("gzip", false, "Datei mit gzip packen")
	out := fs.String("o", "", "Ausgabeordner")
	fs.Parse(args)
	if *key == "" || *img == "" || *ver == "" || *out == "" {
		return fmt.Errorf("-key, -image, -version und -o sind nötig")
	}
	seed, err := readSeed(*key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	f, err := os.Open(*img)
	if err != nil {
		return err
	}
	off, err := stickwrite.FindReservation(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", *img, err)
	}
	imgSize, imgSum, err := sumFile(*img)
	if err != nil {
		return err
	}
	fileName := fmt.Sprintf("%s-%s.img", *name, *ver)
	comp := manifest.None
	filePath := filepath.Join(*out, fileName)
	if *gz {
		fileName += ".gz"
		filePath += ".gz"
		comp = manifest.Gzip
		if err := gzipFile(*img, filePath); err != nil {
			return err
		}
	} else if err := copyFile(*img, filePath); err != nil {
		return err
	}
	fileSize, fileSum, err := sumFile(filePath)
	if err != nil {
		return err
	}
	m := &manifest.Manifest{Schema: manifest.Schema, Name: *name, Version: *ver,
		Created:   time.Now().UTC().Format(time.RFC3339),
		File:      manifest.File{Name: fileName, URL: *url, Size: fileSize, SHA256: fileSum, Compression: comp},
		Image:     manifest.Image{Size: imgSize, SHA256: imgSum},
		StickJSON: manifest.StickJSON{Offset: off, Size: stick.Size}}
	data, err := m.Encode()
	if err != nil {
		return err
	}
	sig, err := manifest.Sign(data, seed)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "manifest.json"), data, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "manifest.json.sig"), sig, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %s, stick.json bei %d, Schlüssel %s\n", *out, fileName, off, manifest.Public(seed))
	return nil
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw, _ := gzip.NewWriterLevel(out, gzip.BestCompression)
	if _, err := io.Copy(zw, in); err != nil {
		out.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	pub := fs.String("pub", "", "öffentlicher Schlüssel (base64)")
	fs.Parse(args)
	if *pub == "" || fs.NArg() != 1 {
		return fmt.Errorf("Aufruf: verify -pub <schlüssel> manifest.json")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(fs.Arg(0) + ".sig")
	if err != nil {
		return err
	}
	m, err := manifest.Verify(data, sig, []manifest.PublicKey{manifest.PublicKey(*pub)})
	if err != nil {
		return err
	}
	fmt.Printf("gültig: %s %s, %s (%d Bytes)\n", m.Name, m.Version, m.File.Name, m.File.Size)
	return nil
}
