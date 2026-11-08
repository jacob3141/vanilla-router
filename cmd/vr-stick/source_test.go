// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vanilla-router/internal/manifest"
	"vanilla-router/internal/stick"
	"vanilla-router/internal/stickwrite"
	"vanilla-router/internal/windisk"
)

var seed = bytes.Repeat([]byte{42}, 32)

type fixture struct {
	img, file, mdata, sig []byte
	m                     *manifest.Manifest
}

func newFixture(t *testing.T, version string) *fixture {
	t.Helper()
	img := make([]byte, 3<<20)
	rand.New(rand.NewSource(int64(len(version)))).Read(img)
	copy(img[2<<20:], bytes.Repeat([]byte{' '}, stick.Size))
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(img)
	zw.Close()
	sum := func(b []byte) string { s := sha256.Sum256(b); return manifest.HexSum(s[:]) }
	m := &manifest.Manifest{Schema: 1, Name: "vanilla-router-stick", Version: version,
		File:      manifest.File{Name: "vanilla-router-stick-" + version + ".img.gz", Size: int64(buf.Len()), SHA256: sum(buf.Bytes()), Compression: manifest.Gzip},
		Image:     manifest.Image{Size: int64(len(img)), SHA256: sum(img)},
		StickJSON: manifest.StickJSON{Offset: 2 << 20, Size: stick.Size}}
	data, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := manifest.Sign(data, seed)
	return &fixture{img: img, file: buf.Bytes(), mdata: data, sig: sig, m: m}
}

type server struct {
	*httptest.Server
	mu     sync.Mutex
	f      *fixture
	ranges []string
}

func newServer(t *testing.T, f *fixture) *server {
	s := &server{f: f}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		f := s.f
		if r.Header.Get("Range") != "" {
			s.ranges = append(s.ranges, r.Header.Get("Range"))
		}
		s.mu.Unlock()
		switch r.URL.Path {
		case "/stick/manifest.json":
			w.Write(f.mdata)
		case "/stick/manifest.json.sig":
			w.Write(f.sig)
		case "/stick/" + f.m.File.Name:
			http.ServeContent(w, r, f.m.File.Name, time.Time{}, bytes.NewReader(f.file))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func testKeys() []manifest.PublicKey { return []manifest.PublicKey{manifest.Public(seed)} }

func TestManifestAndOffline(t *testing.T) {
	f := newFixture(t, "0.1.0")
	srv := newServer(t, f)
	cache := t.TempDir()
	src := newSource(srv.URL+"/stick/manifest.json", cache, testKeys())
	m, offline, err := src.manifest(context.Background())
	if err != nil || offline || m.Version != "0.1.0" {
		t.Fatalf("%v %v %v", m, offline, err)
	}
	srv.Close()
	m, offline, err = src.manifest(context.Background())
	if err != nil || !offline || m.Version != "0.1.0" {
		t.Fatalf("offline: %v %v %v", m, offline, err)
	}
	// Without a cache: a clear message
	src2 := newSource(srv.URL+"/stick/manifest.json", t.TempDir(), testKeys())
	if _, _, err := src2.manifest(context.Background()); !errors.Is(err, errOffline) {
		t.Fatal(err)
	}
}

func TestForeignKey(t *testing.T) {
	f := newFixture(t, "0.1.0")
	f.sig, _ = manifest.Sign(f.mdata, bytes.Repeat([]byte{1}, 32))
	srv := newServer(t, f)
	src := newSource(srv.URL+"/stick/manifest.json", t.TempDir(), testKeys())
	if _, _, err := src.manifest(context.Background()); !errors.Is(err, manifest.ErrSignature) {
		t.Fatal(err)
	}
	if _, _, err := newSource(srv.URL+"/stick/manifest.json", "", nil).manifest(context.Background()); !errors.Is(err, errNoKeys) {
		t.Fatal(err)
	}
	if _, _, err := newSource("", "", testKeys()).manifest(context.Background()); !errors.Is(err, errNoSource) {
		t.Fatal(err)
	}
}

func TestDownloadResumeAndCleanup(t *testing.T) {
	f := newFixture(t, "0.2.0")
	srv := newServer(t, f)
	cache := t.TempDir()
	os.WriteFile(filepath.Join(cache, "vanilla-router-stick-0.1.0.img.gz"), []byte("alt"), 0o644)
	part := filepath.Join(cache, f.m.File.Name+".part")
	os.WriteFile(part, f.file[:len(f.file)/2], 0o644)
	src := newSource(srv.URL+"/stick/manifest.json", cache, testKeys())
	var last int64
	im, err := src.download(context.Background(), f.m, func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(im.path)
	if !bytes.Equal(got, f.file) || last != f.m.File.Size || !im.cached {
		t.Fatal("wrong file")
	}
	if len(srv.ranges) != 1 || !strings.HasPrefix(srv.ranges[0], "bytes=") {
		t.Errorf("no resume: %v", srv.ranges)
	}
	if _, err := os.Stat(filepath.Join(cache, "vanilla-router-stick-0.1.0.img.gz")); !os.IsNotExist(err) {
		t.Error("old image not deleted")
	}
	// The second call downloads nothing.
	srv.Close()
	if _, err := src.download(context.Background(), f.m, nil); err != nil {
		t.Fatal(err)
	}
}

// fakeDev is a stick in memory.
type fakeDev struct {
	b      []byte
	closed bool
}

func (d *fakeDev) WriteAt(p []byte, off int64) (int, error) { return copy(d.b[off:], p), nil }
func (d *fakeDev) ReadAt(p []byte, off int64) (int, error)  { return copy(p, d.b[off:]), nil }
func (d *fakeDev) Size() int64                              { return int64(len(d.b)) }
func (d *fakeDev) SectorSize() int                          { return 512 }
func (d *fakeDev) Close() error                             { d.closed = true; return nil }

func runJob(t *testing.T, src *source, im *image, d windisk.Disk, p platform) jobState {
	t.Helper()
	sj, _ := stick.Info{Schema: 1, Keymap: "de", Timezone: "Europe/Berlin"}.Encode()
	j := &job{}
	run(context.Background(), j, src, im, d, p, sj)
	return j.state()
}

func TestFlow(t *testing.T) {
	f := newFixture(t, "0.1.0")
	srv := newServer(t, f)
	cache := t.TempDir()
	src := newSource(srv.URL+"/stick/manifest.json", cache, testKeys())

	pj := &job{}
	prepare(context.Background(), pj, src, nil)
	ps := pj.state()
	if ps.err != nil || ps.im == nil || ps.im.path != "" {
		t.Fatalf("%+v", ps)
	}
	dev := &fakeDev{b: make([]byte, 8<<20)}
	d := windisk.Disk{Number: 3, Size: dev.Size(), Sector: 512}
	p := platform{open: func(windisk.Disk) (device, error) { return dev, nil }, disksOfPath: func(string) []int { return []int{0} }}
	s := runJob(t, src, ps.im, d, p)
	if s.err != nil || !s.wrote || !dev.closed {
		t.Fatalf("%+v", s)
	}
	in, err := stick.Parse(dev.b[2<<20 : 2<<20+stick.Size])
	if err != nil || in.Timezone != "Europe/Berlin" {
		t.Fatalf("stick.json: %+v %v", in, err)
	}
	if !bytes.Equal(dev.b[:2<<20], f.img[:2<<20]) {
		t.Error("wrong content")
	}

	// Stick too small: download nothing, write nothing
	small := windisk.Disk{Number: 3, Size: 1 << 20}
	if s := runJob(t, src, ps.im, small, p); !errors.Is(s.err, stickwrite.ErrTooSmall) || s.wrote {
		t.Fatalf("%+v", s)
	}

	// Image is on the target stick
	p2 := p
	p2.disksOfPath = func(string) []int { return []int{3} }
	if s := runJob(t, src, ps.im, d, p2); !errors.Is(s.err, errOnStick) || s.wrote {
		t.Fatalf("%+v", s)
	}

	// A damaged image in the cache is deleted
	cached := filepath.Join(cache, f.m.File.Name)
	bad := append([]byte(nil), f.file...)
	bad[100] ^= 1
	os.WriteFile(cached, bad, 0o644)
	if s := runJob(t, src, ps.im, d, p); !errors.Is(s.err, stickwrite.ErrCorrupt) || s.wrote {
		t.Fatalf("%+v", s)
	}
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Error("damaged image not deleted")
	}
	if txt := errText(stickwrite.ErrCorrupt, false, false); !strings.Contains(txt, "downloaded") {
		t.Error(txt)
	}
}

func TestLocalFile(t *testing.T) {
	f := newFixture(t, "0.1.0")
	dir := t.TempDir()
	path := filepath.Join(dir, "image.img.gz")
	os.WriteFile(path, f.file, 0o644)
	if _, err := localImage(path, testKeys()); !errors.Is(err, errLocalManifest) {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, manifestName), f.mdata, 0o644)
	os.WriteFile(filepath.Join(dir, sigName), f.sig, 0o644)
	im, err := localImage(path, testKeys())
	if err != nil || im.cached {
		t.Fatal(err)
	}
	os.WriteFile(path, f.file[:100], 0o644)
	if _, err := localImage(path, testKeys()); !errors.Is(err, errLocalMismatch) {
		t.Fatal(err)
	}
}

func TestTexts(t *testing.T) {
	for k, v := range texts {
		if v == "" {
			t.Errorf("%s: text is missing", k)
		}
	}
	{
		for _, e := range []error{windisk.ErrWriteProtected, windisk.ErrRemoved, windisk.ErrBusy, windisk.ErrChanged,
			windisk.ErrNotAdmin, stickwrite.ErrVerify, stickwrite.ErrNotBlank, errOffline, errDownload, errNoSource,
			errNoKeys, manifest.ErrSignature, errLocalManifest, errLocalMismatch, errOnStick} {
			if txt := errText(e, false, false); strings.Contains(txt, "%!") || strings.HasPrefix(txt, tr("err_other", "")) {
				t.Errorf("%v: %q", e, txt)
			}
		}
		if errText(context.Canceled, false, true) != tr("canceled_wrote") {
			t.Error("abort")
		}
	}
	if size(32_017_047_552) != "32 GB" || size(7_700_000_000) != "7.7 GB" || size(500_000_000) != "500 MB" {
		t.Error(size(32_017_047_552), size(7_700_000_000))
	}
	if diskLabel(windisk.Disk{Vendor: "SanDisk", Product: "Ultra", Size: 32e9, Letters: []string{"E:"}}) != "SanDisk Ultra (32 GB), drive E:" {
		t.Error(diskLabel(windisk.Disk{Vendor: "SanDisk", Product: "Ultra", Size: 32e9, Letters: []string{"E:"}}))
	}
}
