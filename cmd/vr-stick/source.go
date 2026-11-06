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

// Where the image comes from (F-STICK-04, F-STICK-08). manifestURL and publicKeys are set at build time
// (-ldflags "-X main.manifestURL=… -X main.publicKeys=…") until Q-19 is decided.
var (
	manifestURL string
	publicKeys  string // base64, comma-separated
	version     = "dev"
)

const (
	manifestName = "manifest.json"
	sigName      = "manifest.json.sig"
	maxManifest  = 64 << 10
)

var (
	errNoSource      = errors.New("no download URL built in")
	errNoKeys        = errors.New("no key built in")
	errOffline       = errors.New("no connection to the download")
	errLocalManifest = errors.New("manifest.json with signature is missing next to the image file")
	errLocalMismatch = errors.New("the image file does not match the manifest")
	errDownload      = errors.New("download cancelled")
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

// source downloads the manifest and image and keeps both in the cache.
type source struct {
	url    string
	keys   []manifest.PublicKey
	cache  string // folder, e.g. %LOCALAPPDATA%\vanilla-router\stick
	client *http.Client
}

// image is an image that can be written.
type image struct {
	m      *manifest.Manifest
	path   string
	cached bool // in the cache: delete on a checksum error
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
		err = errors.New("response too big")
	}
	return b, err
}

// manifest fetches the current manifest. Without a connection the last downloaded one applies (offline, F-STICK-08).
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
		err = errors.New("too big")
	}
	return b, err
}

// download fetches the file into the cache, resumes an interrupted download and
// removes older images. If the file is already complete, nothing is downloaded; stickwrite.Check
// verifies the checksum afterwards.
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
		return nil, fmt.Errorf("%w: %d of %d bytes", errDownload, have, m.File.Size)
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

// prune deletes older images from the cache, so that several GB do not pile up.
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

// localImage checks an image file the user chose (offline, F-STICK-08). manifest.json and
// manifest.json.sig must be next to it, just as they are on the download page.
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

// discardCorrupt deletes a damaged image from the cache, so that the next attempt
// downloads it again.
func discardCorrupt(im *image, err error) {
	if im != nil && im.cached && errors.Is(err, stickwrite.ErrCorrupt) {
		os.Remove(im.path)
	}
}
