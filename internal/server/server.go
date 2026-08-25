package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	internalarchive "github.com/billy4479/agent-up/internal/archive"
)

type manifest struct {
	Kind      string     `json:"kind"`
	Filename  string     `json:"filename,omitempty"`
	MIME      string     `json:"mime,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type Server struct {
	dataDir       string
	maxUploadSize int64
	uploadTTL     time.Duration
}

type listingEntry struct {
	Name string
	Href string
	Dir  bool
}

var listingTemplate = template.Must(template.New("listing").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Index of {{.Path}}</title></head>
<body><h1>Index of {{.Path}}</h1><ul>{{if .Parent}}<li><a href="../">../</a></li>{{end}}{{range .Entries}}<li><a href="{{.Href}}">{{.Name}}{{if .Dir}}/{{end}}</a></li>{{end}}</ul></body></html>`))

func New(dataDir string, maxUploadSize int64, uploadTTL time.Duration) (*Server, error) {
	if maxUploadSize < 0 {
		return nil, fmt.Errorf("maximum upload size cannot be negative")
	}
	if uploadTTL <= 0 {
		return nil, fmt.Errorf("upload TTL must be positive")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	return &Server{dataDir: dataDir, maxUploadSize: maxUploadSize, uploadTTL: uploadTTL}, nil
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path == "/api/uploads" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleUpload(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handlePublic(w, r)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	name := r.URL.Query().Get("name")
	if kind != "file" && kind != "directory" {
		http.Error(w, "kind must be file or directory", http.StatusBadRequest)
		return
	}
	if kind == "file" && !validFilename(name) {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}
	if s.maxUploadSize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadSize)
	}

	staging, err := os.MkdirTemp(s.dataDir, ".upload-")
	if err != nil {
		http.Error(w, "could not create staging directory", http.StatusInternalServerError)
		return
	}
	defer func() { _ = os.RemoveAll(staging) }()
	contentDir := filepath.Join(staging, "content")
	if err := os.Mkdir(contentDir, 0o755); err != nil {
		http.Error(w, "could not create upload", http.StatusInternalServerError)
		return
	}

	m := manifest{Kind: kind, Filename: name}
	if kind == "file" {
		m.MIME = r.Header.Get("Content-Type")
		file, err := os.OpenFile(filepath.Join(contentDir, "file"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, err = io.Copy(file, r.Body)
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}
		if err != nil {
			s.writeUploadError(w, err)
			return
		}
	} else if err := internalarchive.ExtractDirectory(r.Body, contentDir); err != nil {
		s.writeUploadError(w, err)
		return
	}

	expiresAt := time.Now().Add(s.uploadTTL)
	m.ExpiresAt = &expiresAt
	manifestData, err := json.Marshal(m)
	if err == nil {
		err = os.WriteFile(filepath.Join(staging, "manifest.json"), manifestData, 0o644)
	}
	if err != nil {
		http.Error(w, "could not store manifest", http.StatusInternalServerError)
		return
	}

	slug, err := s.publish(staging)
	if err != nil {
		http.Error(w, "could not publish upload", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"slug": slug})
}

func (s *Server) writeUploadError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		http.Error(w, "upload exceeds maximum size", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "invalid upload: "+err.Error(), http.StatusBadRequest)
}

func (s *Server) publish(staging string) (string, error) {
	for range 20 {
		random := make([]byte, 6)
		if _, err := rand.Read(random); err != nil {
			return "", err
		}
		slug := base64.RawURLEncoding.EncodeToString(random)
		err := os.Rename(staging, filepath.Join(s.dataDir, slug))
		if err == nil {
			return slug, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("could not allocate unique slug")
}

func (s *Server) handlePublic(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if trimmed == "" || parts[0] == "" || !validSlug(parts[0]) {
		http.NotFound(w, r)
		return
	}
	slug := parts[0]
	uploadDir := filepath.Join(s.dataDir, slug)
	m, err := readManifest(filepath.Join(uploadDir, "manifest.json"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	expiresAt, err := s.expiration(uploadDir, m)
	if err != nil || !time.Now().Before(expiresAt) {
		if err == nil {
			_ = os.RemoveAll(uploadDir)
		}
		http.NotFound(w, r)
		return
	}
	expiresAt = time.Now().Add(s.uploadTTL)
	m.ExpiresAt = &expiresAt
	if err := writeManifest(filepath.Join(uploadDir, "manifest.json"), m); err != nil {
		http.Error(w, "could not renew upload", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "public, no-cache, must-revalidate")
	w.Header().Set("Expires", expiresAt.UTC().Format(http.TimeFormat))
	remainder := ""
	if len(parts) == 2 {
		remainder = parts[1]
	}
	if m.Kind == "file" {
		s.serveSingleFile(w, r, slug, remainder, m)
		return
	}
	if m.Kind != "directory" {
		http.NotFound(w, r)
		return
	}
	s.serveDirectory(w, r, slug, remainder)
}

// CleanupExpired removes published uploads whose lifetime has elapsed.
func (s *Server) CleanupExpired() error {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return fmt.Errorf("read data directory: %w", err)
	}
	now := time.Now()
	var cleanupErrors []error
	for _, entry := range entries {
		if !entry.IsDir() || !validSlug(entry.Name()) {
			continue
		}
		uploadDir := filepath.Join(s.dataDir, entry.Name())
		m, err := readManifest(filepath.Join(uploadDir, "manifest.json"))
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("read upload %s: %w", entry.Name(), err))
			continue
		}
		expiresAt, err := s.expiration(uploadDir, m)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect upload %s: %w", entry.Name(), err))
			continue
		}
		if !now.Before(expiresAt) {
			if err := os.RemoveAll(uploadDir); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove upload %s: %w", entry.Name(), err))
			}
		}
	}
	return errors.Join(cleanupErrors...)
}

func (s *Server) expiration(uploadDir string, m manifest) (time.Time, error) {
	if m.ExpiresAt != nil {
		return *m.ExpiresAt, nil
	}
	info, err := os.Stat(uploadDir)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime().Add(s.uploadTTL), nil
}

func (s *Server) serveSingleFile(w http.ResponseWriter, r *http.Request, slug, remainder string, m manifest) {
	if m.Filename == "index.html" && remainder == "" {
		s.serveFile(w, r, filepath.Join(s.dataDir, slug, "content", "file"), m.Filename, m.MIME)
		return
	}
	if remainder != m.Filename {
		http.NotFound(w, r)
		return
	}
	s.serveFile(w, r, filepath.Join(s.dataDir, slug, "content", "file"), m.Filename, m.MIME)
}

func (s *Server) serveDirectory(w http.ResponseWriter, r *http.Request, slug, remainder string) {
	if remainder == "" && !strings.HasSuffix(r.URL.Path, "/") {
		redirectWithQuery(w, r, r.URL.Path+"/")
		return
	}
	cleaned := path.Clean(remainder)
	if cleaned == "." {
		cleaned = ""
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		http.NotFound(w, r)
		return
	}
	target := filepath.Join(s.dataDir, slug, "content", filepath.FromSlash(cleaned))
	info, err := os.Stat(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !info.IsDir() {
		s.serveFile(w, r, target, info.Name(), "")
		return
	}
	if !strings.HasSuffix(r.URL.Path, "/") {
		redirectWithQuery(w, r, r.URL.Path+"/")
		return
	}
	index := filepath.Join(target, "index.html")
	if indexInfo, err := os.Stat(index); err == nil && indexInfo.Mode().IsRegular() {
		s.serveFile(w, r, index, "index.html", "")
		return
	}
	s.serveListing(w, target, cleaned)
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, filename, publicName, explicitMIME string) {
	file, err := os.Open(filename)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if explicitMIME != "" {
		w.Header().Set("Content-Type", explicitMIME)
	} else if mimeType := mime.TypeByExtension(filepath.Ext(publicName)); mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}
	http.ServeContent(w, r, publicName, info.ModTime(), file)
}

func (s *Server) serveListing(w http.ResponseWriter, directory, relative string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		http.Error(w, "could not read directory", http.StatusInternalServerError)
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	items := make([]listingEntry, 0, len(entries))
	for _, entry := range entries {
		href := url.PathEscape(entry.Name())
		if entry.IsDir() {
			href += "/"
		}
		items = append(items, listingEntry{Name: entry.Name(), Href: href, Dir: entry.IsDir()})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = listingTemplate.Execute(w, struct {
		Path    string
		Parent  bool
		Entries []listingEntry
	}{Path: "/" + relative, Parent: relative != "", Entries: items})
}

func readManifest(filename string) (manifest, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return manifest{}, err
	}
	var m manifest
	err = json.Unmarshal(data, &m)
	return m, err
}

func writeManifest(filename string, m manifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".manifest-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err = temporary.Chmod(0o644); err == nil {
		_, err = temporary.Write(data)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryName, filename)
}

func validFilename(name string) bool {
	return name != "" && name != "." && name != ".." && path.Base(name) == name && !strings.Contains(name, "\\")
}

func validSlug(slug string) bool {
	if len(slug) != 8 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(slug)
	return err == nil
}

func redirectWithQuery(w http.ResponseWriter, r *http.Request, location string) {
	if r.URL.RawQuery != "" {
		location += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, location, http.StatusMovedPermanently)
}
