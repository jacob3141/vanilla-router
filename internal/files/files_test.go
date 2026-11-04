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
	outside := filepath.Join(base, "geheim")
	os.MkdirAll(filepath.Join(share, "Fotos"), 0o755)
	os.MkdirAll(outside, 0o755)
	os.WriteFile(filepath.Join(share, "Fotos", "urlaub.jpg"), []byte("JPEG"), 0o644)
	os.WriteFile(filepath.Join(outside, "shadow"), []byte("root:geheim"), 0o644)
	// Symlink, der aus der Freigabe herauszeigt
	os.Symlink(outside, filepath.Join(share, "ausbruch"))
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
		"/download?path=../geheim/shadow",
		"/download?path=/../../geheim/shadow",
		"/download?path=ausbruch/shadow",
		"/list?path=ausbruch",
		"/download?path=" + filepath.Join(outside, "shadow"),
	} {
		w := req(s, "GET", u, nil)
		if strings.Contains(w.Body.String(), "root:geheim") || w.Code == http.StatusOK && strings.Contains(u, "download") {
			t.Errorf("%s: Ausbruch möglich (Status %d): %s", u, w.Code, w.Body)
		}
	}
	// Der Symlink taucht in der Liste gar nicht erst auf
	if w := req(s, "GET", "/list?path=", nil); strings.Contains(w.Body.String(), "ausbruch") {
		t.Errorf("Symlink wird angezeigt: %s", w.Body)
	}
	// Umbenennen nach außen
	if w := req(s, "POST", "/rename", map[string]string{"path": "Fotos/urlaub.jpg", "new_name": "../../geheim/x"}); w.Code == 200 {
		t.Error("Umbenennen mit Pfad im Namen akzeptiert")
	}
	if w := req(s, "POST", "/mkdir", map[string]string{"path": "ausbruch", "name": "neu"}); w.Code == 200 {
		t.Error("Ordner über Symlink außerhalb angelegt")
	}
	if _, err := os.Stat(filepath.Join(outside, "neu")); err == nil {
		t.Error("Ordner außerhalb der Freigabe existiert")
	}
}

func TestListAndDownload(t *testing.T) {
	s, _, _ := setup(t)
	w := req(s, "GET", "/list?path=Fotos", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"urlaub.jpg"`) {
		t.Fatalf("Liste: %d %s", w.Code, w.Body)
	}
	w = req(s, "GET", "/download?path=Fotos/urlaub.jpg", nil)
	if w.Code != 200 || w.Body.String() != "JPEG" {
		t.Fatalf("Download: %d %q", w.Code, w.Body)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("Download nicht als Anhang ausgeliefert: %v", w.Header())
	}
}

func TestDeleteMovesToTrash(t *testing.T) {
	s, share, _ := setup(t)
	if w := req(s, "POST", "/delete", map[string][]string{"paths": {"Fotos/urlaub.jpg"}}); w.Code != 200 {
		t.Fatalf("Löschen: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(share, "Fotos", "urlaub.jpg")); err == nil {
		t.Error("Datei noch am alten Ort")
	}
	trashed := filepath.Join(share, Trash, "2026-11-04_120000", "Fotos", "urlaub.jpg")
	if _, err := os.Stat(trashed); err != nil {
		t.Fatalf("Datei nicht im Papierkorb: %v", err)
	}
	// Im Papierkorb löschen = endgültig
	if w := req(s, "POST", "/delete", map[string][]string{"paths": {Trash + "/2026-11-04_120000"}}); w.Code != 200 {
		t.Fatalf("Leeren: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(trashed); err == nil {
		t.Error("Datei nach endgültigem Löschen noch da")
	}
	for _, p := range []string{"", "/", Trash, "."} {
		if w := req(s, "POST", "/delete", map[string][]string{"paths": {p}}); w.Code == 200 {
			t.Errorf("Löschen von %q akzeptiert", p)
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
		r := httptest.NewRequest("POST", "/upload?path=Fotos", &buf)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := upload("neu.txt", "hallo"); w.Code != 200 {
		t.Fatalf("Upload: %d %s", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(share, "Fotos", "neu.txt")); string(b) != "hallo" {
		t.Errorf("Inhalt falsch: %q", b)
	}
	if w := upload("neu.txt", "anders"); w.Code == 200 {
		t.Error("vorhandene Datei überschrieben")
	}
	// Ein Dateiname mit Pfad landet nur als Name im Zielordner
	if w := upload(`..\..\boese.txt`, "x"); w.Code != 200 {
		t.Fatalf("Upload mit Pfad im Namen: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(share, "Fotos", "boese.txt")); err != nil {
		t.Errorf("Datei nicht im Zielordner: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(share, "Fotos"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".upload-") {
			t.Errorf("Temporäre Datei liegen geblieben: %s", e.Name())
		}
	}
}

func TestValidName(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "a:b", "x?", "ende.", "ende ", strings.Repeat("a", 256)} {
		if validName(bad) == nil {
			t.Errorf("%q akzeptiert", bad)
		}
	}
	for _, ok := range []string{"Urlaub 2026", "Rechnung_März.pdf", ".versteckt"} {
		if err := validName(ok); err != nil {
			t.Errorf("%q abgelehnt: %v", ok, err)
		}
	}
}
