package server

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileUploads(t *testing.T) {
	service, err := New(t.TempDir(), 0, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(service.Handler())
	defer host.Close()

	slug := upload(t, host.URL, "file", "report.pdf", "application/special", strings.NewReader("pdf data"))
	response, err := http.Get(host.URL + "/" + slug + "/report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	assertStatus(t, response, http.StatusOK)
	if got := response.Header.Get("Content-Type"); got != "application/special" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	body, _ := io.ReadAll(response.Body)
	if string(body) != "pdf data" {
		t.Fatalf("body = %q", body)
	}

	htmlSlug := upload(t, host.URL, "file", "index.html", "", strings.NewReader("<h1>site</h1>"))
	response, err = http.Get(host.URL + "/" + htmlSlug)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	assertStatus(t, response, http.StatusOK)
	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestDirectoryBrowsing(t *testing.T) {
	service, err := New(t.TempDir(), 0, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(service.Handler())
	defer host.Close()

	archive := makeTar(t, []tarEntry{
		{name: "a file.txt", body: "root"},
		{name: "café.txt", body: "unicode"},
		{name: "nested/", directory: true},
		{name: "nested/index.html", body: `<a href="../a file.txt">home</a>`},
	})
	slug := upload(t, host.URL, "directory", "", "application/x-tar", bytes.NewReader(archive))

	client := *http.DefaultClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(host.URL + "/" + slug + "?download=no")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	assertStatus(t, response, http.StatusMovedPermanently)
	if got := response.Header.Get("Location"); got != "/"+slug+"/?download=no" {
		t.Fatalf("Location = %q", got)
	}

	response, err = http.Get(host.URL + "/" + slug + "/")
	if err != nil {
		t.Fatal(err)
	}
	listing, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	assertStatus(t, response, http.StatusOK)
	for _, wanted := range []string{"a file.txt", "café.txt", "nested/"} {
		if !strings.Contains(string(listing), wanted) {
			t.Fatalf("listing does not contain %q: %s", wanted, listing)
		}
	}

	response, err = http.Get(host.URL + "/" + slug + "/nested/")
	if err != nil {
		t.Fatal(err)
	}
	index, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if !strings.Contains(string(index), "home") {
		t.Fatalf("index body = %q", index)
	}

	response, err = http.Get(host.URL + "/" + slug + "/" + url.PathEscape("café.txt"))
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(content) != "unicode" {
		t.Fatalf("file body = %q", content)
	}
}

func TestInvalidArchivesAreNotPublished(t *testing.T) {
	dataDir := t.TempDir()
	service, err := New(dataDir, 0, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		body []byte
	}{
		{name: "malformed", body: []byte("not a tar archive")},
		{name: "traversal", body: makeTar(t, []tarEntry{{name: "../outside", body: "bad"}})},
		{name: "duplicate", body: makeTar(t, []tarEntry{{name: "same", body: "one"}, {name: "same", body: "two"}})},
		{name: "link", body: makeLinkTar(t)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/uploads?kind=directory", bytes.NewReader(test.body))
			response := httptest.NewRecorder()
			service.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			assertDataDirEmpty(t, dataDir)
		})
	}
}

func TestInterruptedFileUploadIsNotPublished(t *testing.T) {
	dataDir := t.TempDir()
	service, err := New(dataDir, 0, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/uploads?kind=file&name=file.txt", errorReader{})
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
	assertDataDirEmpty(t, dataDir)
}

func TestMaximumUploadSize(t *testing.T) {
	for _, test := range []struct {
		name  string
		query string
		body  []byte
		limit int64
	}{
		{name: "file", query: "kind=file&name=file.txt", body: []byte("12345"), limit: 4},
		{name: "directory", query: "kind=directory", body: makeTar(t, []tarEntry{{name: "file", body: "content"}}), limit: 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			service, err := New(dataDir, test.limit, 24*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/uploads?"+test.query, bytes.NewReader(test.body))
			response := httptest.NewRecorder()
			service.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			assertDataDirEmpty(t, dataDir)
		})
	}
}

func TestUploadsExpire(t *testing.T) {
	dataDir := t.TempDir()
	service, err := New(dataDir, 0, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(service.Handler())
	defer host.Close()

	createdAt := time.Now()
	slug := upload(t, host.URL, "file", "report.txt", "text/plain", strings.NewReader("report"))
	manifestPath := filepath.Join(dataDir, slug, "manifest.json")
	m, err := readManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if m.ExpiresAt == nil || m.ExpiresAt.Before(createdAt.Add(2*time.Hour)) || m.ExpiresAt.After(time.Now().Add(2*time.Hour)) {
		t.Fatalf("expires_at = %v, want approximately two hours after creation", m.ExpiresAt)
	}

	response, err := http.Get(host.URL + "/" + slug + "/report.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	assertStatus(t, response, http.StatusOK)
	if got := response.Header.Get("Cache-Control"); got != "public, no-cache, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Header.Get("Expires"); got == "" {
		t.Fatal("Expires header is empty")
	}

	soon := time.Now().Add(time.Minute)
	m.ExpiresAt = &soon
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	renewedAfter := time.Now().Add(2 * time.Hour)
	response, err = http.Get(host.URL + "/" + slug + "/report.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	assertStatus(t, response, http.StatusOK)
	if got := response.Header.Get("Cache-Control"); got != "public, no-cache, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
	m, err = readManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if m.ExpiresAt == nil || m.ExpiresAt.Before(renewedAfter) || m.ExpiresAt.After(time.Now().Add(2*time.Hour)) {
		t.Fatalf("expires_at = %v, want approximately two hours after access", m.ExpiresAt)
	}

	expiredAt := time.Now().Add(-time.Minute)
	m.ExpiresAt = &expiredAt
	data, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	response, err = http.Get(host.URL + "/" + slug + "/report.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	assertStatus(t, response, http.StatusNotFound)
	if _, err := os.Stat(filepath.Join(dataDir, slug)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired upload still exists: %v", err)
	}
}

func TestCleanupExpiredLegacyUploads(t *testing.T) {
	dataDir := t.TempDir()
	service, err := New(dataDir, 0, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	oldSlug := "AAAAAAAA"
	newSlug := "BBBBBBBB"
	writeLegacyUpload(t, dataDir, oldSlug)
	writeLegacyUpload(t, dataDir, newSlug)
	oldTime := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(filepath.Join(dataDir, oldSlug), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(dataDir, ".upload-in-progress")
	if err := os.Mkdir(staging, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := service.CleanupExpired(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, oldSlug)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired legacy upload still exists: %v", err)
	}
	for _, path := range []string{filepath.Join(dataDir, newSlug), staging} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unexpired or staging directory was removed: %v", err)
		}
	}
}

func TestNewRejectsInvalidUploadTTL(t *testing.T) {
	if _, err := New(t.TempDir(), 0, 0); err == nil {
		t.Fatal("New accepted a zero upload TTL")
	}
}

type tarEntry struct {
	name      string
	body      string
	directory bool
}

func makeTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var result bytes.Buffer
	writer := tar.NewWriter(&result)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}
		if entry.directory {
			header.Mode = 0o755
			header.Size = 0
			header.Typeflag = tar.TypeDir
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func makeLinkTar(t *testing.T) []byte {
	t.Helper()
	var result bytes.Buffer
	writer := tar.NewWriter(&result)
	if err := writer.WriteHeader(&tar.Header{Name: "link", Linkname: "target", Typeflag: tar.TypeSymlink}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func upload(t *testing.T, baseURL, kind, name, mimeType string, body io.Reader) string {
	t.Helper()
	endpoint := baseURL + "/api/uploads?kind=" + url.QueryEscape(kind)
	if name != "" {
		endpoint += "&name=" + url.QueryEscape(name)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "" {
		request.Header.Set("Content-Type", mimeType)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	assertStatus(t, response, http.StatusCreated)
	var result struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result.Slug
}

func assertStatus(t *testing.T, response *http.Response, wanted int) {
	t.Helper()
	if response.StatusCode != wanted {
		t.Fatalf("status = %d, want %d", response.StatusCode, wanted)
	}
}

func assertDataDirEmpty(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("data directory contains %v", entries)
	}
	if _, err := os.Stat(filepath.Join(directory, "..", "outside")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file exists or stat failed: %v", err)
	}
}

func writeLegacyUpload(t *testing.T, dataDir, slug string) {
	t.Helper()
	uploadDir := filepath.Join(dataDir, slug)
	if err := os.Mkdir(uploadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest{Kind: "file", Filename: "file.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("interrupted") }
