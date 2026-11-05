// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vanilla-router/internal/manifest"
	"vanilla-router/internal/stickwrite"
)

// Woher das Abbild kommt (F-STICK-04, F-STICK-08). manifestURL und publicKeys werden beim Bauen gesetzt
// (-ldflags "-X main.manifestURL=… -X main.publicKeys=…"), bis Q-19 entschieden ist.
var (
	manifestURL string
	publicKeys  string // base64, mit Komma getrennt
	version     = "dev"
)

const (
	manifestName = "manifest.json"
	sigName      = "manifest.json.sig"
	maxManifest  = 64 << 10
)

var (
	errNoSource      = errors.New("keine Download-Adresse eingebaut")
	errNoKeys        = errors.New("kein Schlüssel eingebaut")
	errOffline       = errors.New("keine Verbindung zum Download")
	errLocalManifest = errors.New("neben der Abbild-Datei fehlt manifest.json mit Signatur")
	errLocalMismatch = errors.New("die Abbild-Datei passt nicht zum Manifest")
	errDownload      = errors.New("Download abgebrochen")
)

func keys() []manifest.PublicKey {
	var ks []manifest.PublicKey
	for _, k := range strings.Split(publicKeys, ",") {
		if k = strings.TrimSpace(k); k != "" {
			ks = append(ks, manifest.PublicKey(k))
		}
	}
	return ks
}

// source lädt Manifest und Abbild und hält beides im Zwischenspeicher.
type source struct {
	url    string
	keys   []manifest.PublicKey
	cache  string // Ordner, z. B. %LOCALAPPDATA%\vanilla-router\stick
	client *http.Client
}

// image ist ein Abbild, das geschrieben werden kann.
type image struct {
	m      *manifest.Manifest
	path   string
	cached bool // im Zwischenspeicher: bei einem Prüfsummenfehler löschen
}

func (im *image) open() (io.ReadCloser, error) { return os.Open(im.path) }

func newSource(url, cache string, ks []manifest.PublicKey) *source {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 30 * time.Second
	tr.Proxy = http.ProxyFromEnvironment
	return &source{url: url, keys: ks, cache: cache, client: &http.Client{Transport: tr}}
}

func (s *source) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "vanilla-router-stick/"+version)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("Antwort zu groß")
	}
	return b, err
}

// manifest holt das aktuelle Manifest. Ohne Verbindung gilt das zuletzt geladene (offline, F-STICK-08).
func (s *source) manifest(ctx context.Context) (*manifest.Manifest, bool, error) {
	if len(s.keys) == 0 {
		return nil, false, errNoKeys
	}
	if s.url == "" {
		return nil, false, errNoSource
	}
	data, err := s.get(ctx, s.url, maxManifest)
	var sig []byte
	if err == nil {
		sig, err = s.get(ctx, s.url+".sig", maxManifest)
	}
	if err == nil {
		m, verr := manifest.Verify(data, sig, s.keys)
		if verr != nil {
			return nil, false, verr
		}
		if s.cache != "" && os.MkdirAll(s.cache, 0o755) == nil {
			writeAtomic(filepath.Join(s.cache, sigName), sig)
			writeAtomic(filepath.Join(s.cache, manifestName), data)
		}
		return m, false, nil
	}
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	if m, cerr := readManifest(s.cache, s.keys); cerr == nil {
		return m, true, nil
	}
	return nil, false, fmt.Errorf("%w: %v", errOffline, err)
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readManifest(dir string, ks []manifest.PublicKey) (*manifest.Manifest, error) {
	if dir == "" {
		return nil, errLocalManifest
	}
	data, err := readLimited(filepath.Join(dir, manifestName))
	if err != nil {
		return nil, errLocalManifest
	}
	sig, err := readLimited(filepath.Join(dir, sigName))
	if err != nil {
		return nil, errLocalManifest
	}
	return manifest.Verify(data, sig, ks)
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxManifest+1))
	if err == nil && len(b) > maxManifest {
		err = errors.New("zu groß")
	}
	return b, err
}

// download lädt die Datei in den Zwischenspeicher, setzt einen abgebrochenen Download fort und
// entfernt ältere Abbilder. Liegt die Datei schon vollständig da, wird nichts geladen; die Prüfsumme
// prüft danach stickwrite.Check.
func (s *source) download(ctx context.Context, m *manifest.Manifest, prog func(done, total int64)) (*image, error) {
	if err := os.MkdirAll(s.cache, 0o755); err != nil {
		return nil, err
	}
	final := filepath.Join(s.cache, m.File.Name)
	im := &image{m: m, path: final, cached: true}
	if st, err := os.Stat(final); err == nil && st.Size() == m.File.Size {
		s.prune(m.File.Name)
		return im, nil
	}
	u, err := m.FileURL(s.url)
	if err != nil {
		return nil, err
	}
	part := final + ".part"
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	have, _ := f.Seek(0, io.SeekEnd)
	if have > m.File.Size {
		have = 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "vanilla-router-stick/"+version)
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", errDownload, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusPartialContent && have > 0 && strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", have)):
	case resp.StatusCode == http.StatusOK:
		have = 0
	default:
		return nil, fmt.Errorf("%w: %s", errDownload, resp.Status)
	}
	if err := f.Truncate(have); err != nil {
		return nil, err
	}
	if _, err := f.Seek(have, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, 1<<20)
	body := io.LimitReader(resp.Body, m.File.Size-have)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return nil, err
			}
			have += int64(n)
			if prog != nil {
				prog(have, m.File.Size)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("%w: %v", errDownload, rerr)
		}
	}
	if have != m.File.Size {
		return nil, fmt.Errorf("%w: %d von %d Bytes", errDownload, have, m.File.Size)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(part, final); err != nil {
		return nil, err
	}
	s.prune(m.File.Name)
	return im, nil
}

// prune löscht ältere Abbilder aus dem Zwischenspeicher, damit nicht mehrere GB liegen bleiben.
func (s *source) prune(keep string) {
	ents, err := os.ReadDir(s.cache)
	if err != nil {
		return
	}
	for _, e := range ents {
		n := e.Name()
		if e.Type().IsRegular() && n != keep && n != manifestName && n != sigName && n != keep+".part" {
			os.Remove(filepath.Join(s.cache, n))
		}
	}
}

// localImage prüft eine selbst gewählte Abbild-Datei (offline, F-STICK-08). Daneben müssen
// manifest.json und manifest.json.sig liegen, so wie sie auf der Download-Seite stehen.
func localImage(path string, ks []manifest.PublicKey) (*image, error) {
	if len(ks) == 0 {
		return nil, errNoKeys
	}
	m, err := readManifest(filepath.Dir(path), ks)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() != m.File.Size {
		return nil, errLocalMismatch
	}
	return &image{m: m, path: path}, nil
}

// discardCorrupt löscht ein beschädigtes Abbild aus dem Zwischenspeicher, damit der nächste Versuch
// neu lädt.
func discardCorrupt(im *image, err error) {
	if im != nil && im.cached && errors.Is(err, stickwrite.ErrCorrupt) {
		os.Remove(im.path)
	}
}
