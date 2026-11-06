// SPDX-License-Identifier: AGPL-3.0-or-later

package files

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) (*Server, string, string) {
	t.Helper()
	base := t.TempDir()
	share := filepath.Join(base, "share")
	outside := filepath.Join(base, "secret")
	os.MkdirAll(filepath.Join(share, "Photos"), 0o755)
	os.MkdirAll(outside, 0o755)
	os.WriteFile(filepath.Join(share, "Photos", "holiday.jpg"), []byte("JPEG"), 0o644)
	os.WriteFile(filepath.Join(outside, "shadow"), []byte("root:secret"), 0o644)
	// Symlink that points out of the share
	os.Symlink(outside, filepath.Join(share, "escape"))
	s, err := New(share, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) }
	return s, share, outside
}

func req(s *Server, method, url string, body any) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(method, url, rd))
	return w
}

func TestNoEscape(t *testing.T) {
	s, _, outside := setup(t)
	for _, u := range []string{
		"/download?path=../secret/shadow",
		"/download?path=/../../secret/shadow",
		"/download?path=escape/shadow",
		"/list?path=escape",
		"/download?path=" + filepath.Join(outside, "shadow"),
	} {
		w := req(s, "GET", u, nil)
		if strings.Contains(w.Body.String(), "root:secret") || w.Code == http.StatusOK && strings.Contains(u, "download") {
			t.Errorf("%s: escape possible (status %d): %s", u, w.Code, w.Body)
		}
	}
	// The symlink does not even appear in the list
	if w := req(s, "GET", "/list?path=", nil); strings.Contains(w.Body.String(), "escape") {
		t.Errorf("symlink is displayed: %s", w.Body)
	}
	// Renaming to the outside
	if w := req(s, "POST", "/rename", map[string]string{"path": "Photos/holiday.jpg", "new_name": "../../secret/x"}); w.Code == 200 {
		t.Error("rename with a path in the name accepted")
	}
	if w := req(s, "POST", "/mkdir", map[string]string{"path": "escape", "name": "new"}); w.Code == 200 {
		t.Error("folder created outside through a symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); err == nil {
		t.Error("folder outside the share exists")
	}
}

func TestListAndDownload(t *testing.T) {
	s, _, _ := setup(t)
	w := req(s, "GET", "/list?path=Photos", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"holiday.jpg"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	w = req(s, "GET", "/download?path=Photos/holiday.jpg", nil)
	if w.Code != 200 || w.Body.String() != "JPEG" {
		t.Fatalf("Download: %d %q", w.Code, w.Body)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("download not delivered as an attachment: %v", w.Header())
	}
}

func TestDeleteMovesToTrash(t *testing.T) {
	s, share, _ := setup(t)
	if w := req(s, "POST", "/delete", map[string][]string{"paths": {"Photos/holiday.jpg"}}); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(share, "Photos", "holiday.jpg")); err == nil {
		t.Error("file still in the old place")
	}
	trashed := filepath.Join(share, Trash, "2026-11-04_120000", "Photos", "holiday.jpg")
	if _, err := os.Stat(trashed); err != nil {
		t.Fatalf("file not in the trash: %v", err)
	}
	// Deleting in the trash = permanent
	if w := req(s, "POST", "/delete", map[string][]string{"paths": {Trash + "/2026-11-04_120000"}}); w.Code != 200 {
		t.Fatalf("emptying: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(trashed); err == nil {
		t.Error("file still there after permanent deletion")
	}
	for _, p := range []string{"", "/", Trash, "."} {
		if w := req(s, "POST", "/delete", map[string][]string{"paths": {p}}); w.Code == 200 {
			t.Errorf("deleting %q accepted", p)
		}
	}
}

func TestUpload(t *testing.T) {
	s, share, _ := setup(t)
	upload := func(name, content string) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", name)
		fw.Write([]byte(content))
		mw.Close()
		r := httptest.NewRequest("POST", "/upload?path=Photos", &buf)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := upload("new.txt", "hello"); w.Code != 200 {
		t.Fatalf("Upload: %d %s", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(share, "Photos", "new.txt")); string(b) != "hello" {
		t.Errorf("wrong content: %q", b)
	}
	if w := upload("new.txt", "different"); w.Code == 200 {
		t.Error("existing file overwritten")
	}
	// A file name with a path only ends up as a name in the target folder
	if w := upload(`..\..\evil.txt`, "x"); w.Code != 200 {
		t.Fatalf("upload with a path in the name: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(share, "Photos", "evil.txt")); err != nil {
		t.Errorf("file not in the target folder: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(share, "Photos"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".upload-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestValidName(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "a:b", "x?", "end.", "end ", strings.Repeat("a", 256)} {
		if validName(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, ok := range []string{"Holiday 2026", "Rechnung_März.pdf", ".hidden"} {
		if err := validName(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
}
