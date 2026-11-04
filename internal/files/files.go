// SPDX-License-Identifier: AGPL-3.0-or-later

// Package files ist der Dateidienst der Oberfläche. Er läuft als smbuser (dieselben Rechte wie
// Samba), hat nur Zugriff auf den Ordner der Freigabe und spricht HTTP über einen Unix-Socket,
// den nur der Webserver erreichen kann. Alle Pfade laufen über os.Root: Das Betriebssystem
// verhindert damit Ausbrüche über "..", absolute Pfade oder Symlinks.
package files

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Trash ist der Papierkorb im Wurzelordner der Freigabe. Löschen verschiebt dorthin.
const Trash = ".papierkorb"

type Server struct {
	root    *os.Root
	rootDir string
	log     *slog.Logger
	now     func() time.Time
}

func New(dir string, log *slog.Logger) (*Server, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Server{root: r, rootDir: dir, log: log, now: time.Now}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /list", s.handleList)
	mux.HandleFunc("GET /download", s.handleDownload)
	mux.HandleFunc("POST /upload", s.handleUpload)
	mux.HandleFunc("POST /mkdir", s.handleMkdir)
	mux.HandleFunc("POST /rename", s.handleRename)
	mux.HandleFunc("POST /delete", s.handleDelete)
	return mux
}

// --- Pfade ---------------------------------------------------------------------------

// cleanPath macht aus einer Angabe der Oberfläche einen relativen Pfad für os.Root ("." = Wurzel).
func cleanPath(p string) (string, error) {
	if !utf8.ValidString(p) || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", errBad("Ungültiger Pfad.")
	}
	c := strings.TrimPrefix(path.Clean("/"+p), "/")
	if c == "" {
		return ".", nil
	}
	return c, nil
}

// validName prüft einen einzelnen Datei- oder Ordnernamen.
func validName(n string) error {
	switch {
	case n == "" || n == "." || n == "..":
		return errBad("Ungültiger Name.")
	case len(n) > 255 || !utf8.ValidString(n):
		return errBad("Der Name ist zu lang.")
	case strings.ContainsAny(n, "/\\\x00"):
		return errBad("Der Name darf keinen Schrägstrich enthalten.")
	case strings.ContainsAny(n, `:*?"<>|`):
		// Diese Zeichen kann Windows über Samba nicht darstellen
		return errBad(`Windows erlaubt die Zeichen : * ? " < > | nicht in Dateinamen.`)
	case strings.TrimSpace(n) != n || strings.HasSuffix(n, "."):
		return errBad("Der Name darf nicht mit Leerzeichen oder Punkt enden (Windows).")
	}
	return nil
}

type userError struct{ msg string }

func (e *userError) Error() string { return e.msg }
func errBad(msg string) error      { return &userError{msg} }

func (s *Server) fail(w http.ResponseWriter, err error) {
	var ue *userError
	status, msg := http.StatusInternalServerError, "Interner Fehler im Dateidienst."
	switch {
	case errors.As(err, &ue):
		status, msg = http.StatusBadRequest, ue.msg
	case errors.Is(err, fs.ErrNotExist):
		status, msg = http.StatusNotFound, "Datei oder Ordner gibt es nicht."
	case errors.Is(err, fs.ErrExist):
		status, msg = http.StatusConflict, "Eine Datei oder ein Ordner mit diesem Namen existiert schon."
	case errors.Is(err, fs.ErrPermission):
		status, msg = http.StatusForbidden, "Keine Berechtigung."
	case strings.Contains(err.Error(), "path escapes"):
		status, msg = http.StatusBadRequest, "Der Pfad zeigt aus der Freigabe heraus."
	case errors.Is(err, syscall.ENOSPC):
		status, msg = http.StatusInsufficientStorage, "Der Speicher ist voll."
	default:
		s.log.Error("dateidienst", "err", err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errBad("Ungültige Anfrage.")
	}
	return nil
}

// --- Auflisten und Herunterladen ------------------------------------------------------

type Entry struct {
	Name     string    `json:"name"`
	Dir      bool      `json:"dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	p, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		s.fail(w, err)
		return
	}
	d, err := s.root.Open(p)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer d.Close()
	des, err := d.ReadDir(-1)
	if err != nil {
		s.fail(w, err)
		return
	}
	entries := make([]Entry, 0, len(des))
	for _, de := range des {
		// Symlinks und Sonderdateien zeigen wir nicht an, sie führen oft aus der Freigabe heraus
		if de.Type()&(fs.ModeSymlink|fs.ModeDevice|fs.ModeNamedPipe|fs.ModeSocket) != 0 {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		e := Entry{Name: de.Name(), Dir: de.IsDir(), Modified: info.ModTime()}
		if !e.Dir {
			e.Size = info.Size()
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	var st syscall.Statfs_t
	var free, total uint64
	if syscall.Statfs(s.rootDir, &st) == nil {
		free, total = st.Bavail*uint64(st.Bsize), st.Blocks*uint64(st.Bsize)
	}
	shown := p
	if shown == "." {
		shown = ""
	}
	writeJSON(w, map[string]any{"path": shown, "entries": entries, "free": free, "total": total, "trash": Trash})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	p, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil || p == "." {
		s.fail(w, errBad("Ungültiger Pfad."))
		return
	}
	f, err := s.root.Open(p)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.fail(w, err)
		return
	}
	if info.IsDir() {
		s.fail(w, errBad("Ordner lassen sich nicht direkt herunterladen."))
		return
	}
	name := path.Base(p)
	// Immer als Download ausliefern, nie im Browser anzeigen: eine HTML-Datei aus der
	// Freigabe darf nicht im Kontext der Oberfläche laufen
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// --- Ändern ---------------------------------------------------------------------------

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	dir, err := cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if info, err := s.root.Stat(dir); err != nil || !info.IsDir() {
		s.fail(w, errBad("Der Zielordner existiert nicht."))
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		s.fail(w, errBad("Ungültiger Upload."))
		return
	}
	var saved []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.fail(w, errBad("Upload abgebrochen."))
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			part.Close()
			continue
		}
		name := filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		if err := validName(name); err != nil {
			part.Close()
			s.fail(w, err)
			return
		}
		target := path.Join(dir, name)
		if _, err := s.root.Lstat(target); err == nil {
			part.Close()
			s.fail(w, &userError{fmt.Sprintf("„%s“ gibt es schon. Bitte vorher umbenennen oder löschen.", name)})
			return
		}
		// Erst in eine versteckte Datei schreiben und am Ende umbenennen: ein abgebrochener
		// Upload hinterlässt keine halbe Datei unter dem richtigen Namen
		tmp := path.Join(dir, fmt.Sprintf(".upload-%d-%s", s.now().UnixNano(), name))
		if len(path.Base(tmp)) > 255 {
			tmp = path.Join(dir, fmt.Sprintf(".upload-%d", s.now().UnixNano()))
		}
		f, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o664)
		if err != nil {
			part.Close()
			s.fail(w, err)
			return
		}
		_, cerr := io.Copy(f, part)
		part.Close()
		if err := f.Close(); cerr == nil {
			cerr = err
		}
		if cerr != nil {
			s.root.Remove(tmp)
			s.fail(w, cerr)
			return
		}
		if err := s.root.Rename(tmp, target); err != nil {
			s.root.Remove(tmp)
			s.fail(w, err)
			return
		}
		saved = append(saved, name)
		s.log.Info("hochgeladen", "datei", target)
	}
	if len(saved) == 0 {
		s.fail(w, errBad("Keine Datei im Upload."))
		return
	}
	writeJSON(w, map[string]any{"ok": true, "saved": saved})
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		s.fail(w, err)
		return
	}
	dir, err := cleanPath(req.Path)
	if err == nil {
		err = validName(req.Name)
	}
	if err == nil {
		err = s.root.Mkdir(path.Join(dir, req.Name), 0o775)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		NewName string `json:"new_name"`
	}
	if err := readJSON(r, &req); err != nil {
		s.fail(w, err)
		return
	}
	p, err := cleanPath(req.Path)
	if err == nil && (p == "." || p == Trash) {
		err = errBad("Dieser Ordner lässt sich nicht umbenennen.")
	}
	if err == nil {
		err = validName(req.NewName)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	target := path.Join(path.Dir(p), req.NewName)
	if _, err := s.root.Lstat(target); err == nil {
		s.fail(w, fs.ErrExist)
		return
	}
	if err := s.root.Rename(p, target); err != nil {
		s.fail(w, err)
		return
	}
	s.log.Info("umbenannt", "von", p, "nach", target)
	writeJSON(w, map[string]any{"ok": true})
}

// handleDelete verschiebt in den Papierkorb: .papierkorb/<zeitpunkt>/<ursprünglicher pfad>.
// Was schon im Papierkorb liegt, wird endgültig gelöscht.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &req); err != nil {
		s.fail(w, err)
		return
	}
	if len(req.Paths) == 0 || len(req.Paths) > 1000 {
		s.fail(w, errBad("Nichts ausgewählt."))
		return
	}
	stamp := s.now().Format("2006-01-02_150405")
	for _, raw := range req.Paths {
		p, err := cleanPath(raw)
		if err == nil && (p == "." || p == Trash) {
			err = errBad("Dieser Ordner lässt sich nicht löschen.")
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		if _, err := s.root.Lstat(p); err != nil {
			s.fail(w, err)
			return
		}
		if strings.HasPrefix(p, Trash+"/") {
			if err := s.root.RemoveAll(p); err != nil {
				s.fail(w, err)
				return
			}
			s.log.Info("endgültig gelöscht", "pfad", p)
			continue
		}
		dst := path.Join(Trash, stamp, p)
		if err := s.root.MkdirAll(path.Dir(dst), 0o775); err != nil {
			s.fail(w, err)
			return
		}
		if err := s.root.Rename(p, dst); err != nil {
			s.fail(w, err)
			return
		}
		s.log.Info("in den papierkorb", "pfad", p)
	}
	writeJSON(w, map[string]any{"ok": true})
}
