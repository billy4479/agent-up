package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	internalserver "github.com/billy4479/agent-up/internal/server"
)

func TestParseArgsAllowsOptionsBeforeAndAfterPath(t *testing.T) {
	for _, args := range [][]string{
		{"--mime", "text/plain", "file.txt"},
		{"file.txt", "--mime", "text/plain"},
		{"--mime=text/plain", "file.txt"},
	} {
		got, err := parseArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		if got.path != "file.txt" || got.mime != "text/plain" {
			t.Fatalf("parseArgs(%v) = %#v", args, got)
		}
	}
}

func TestParseArgsErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: nil, want: "upload path is missing; usage: agent-up [--mime TYPE] [--name FILENAME] PATH"},
		{args: []string{"one", "two"}, want: `multiple upload paths provided: "one" and "two"; provide exactly one`},
		{args: []string{"--unknown", "file"}, want: `unknown option "--unknown"; supported options are --mime and --name`},
		{args: []string{"--mime"}, want: "--mime has no value; provide --mime VALUE"},
		{args: []string{"--mime=", "file"}, want: "--mime is empty; provide a MIME type such as text/plain"},
	} {
		_, err := parseArgs(test.args)
		if err == nil || err.Error() != test.want {
			t.Fatalf("parseArgs(%v) error = %q, want %q", test.args, err, test.want)
		}
	}
}

func TestRunUploadsStdinWithMIMEOverride(t *testing.T) {
	host := newTestServer(t)
	var stdout bytes.Buffer
	err := run(
		[]string{"-", "--name", "image.jpg", "--mime", "image/jpeg"},
		func(name string) string {
			if name == "AGENTUP_URL" {
				return host.URL + "/"
			}
			return ""
		},
		strings.NewReader("image data"),
		&stdout,
		host.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	publicURL := strings.TrimSpace(stdout.String())
	response, err := host.Client().Get(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if got := response.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestRunUploadsDirectory(t *testing.T) {
	host := newTestServer(t)
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "nested", "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := run([]string{directory}, func(string) string { return host.URL }, strings.NewReader(""), &stdout, host.Client()); err != nil {
		t.Fatal(err)
	}
	publicURL := strings.TrimSpace(stdout.String())
	response, err := host.Client().Get(publicURL + "/nested/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestRunUploadsFileWithoutDoubleClose(t *testing.T) {
	host := newTestServer(t)
	filename := filepath.Join(t.TempDir(), "README.md")
	if err := os.WriteFile(filename, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := run([]string{filename}, func(string) string { return host.URL }, strings.NewReader(""), &stdout, host.Client()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(stdout.String()), "/README.md") {
		t.Fatalf("unexpected URL: %q", stdout.String())
	}
}

func TestRunRequiresEnvironmentAndStdinName(t *testing.T) {
	if err := run([]string{"-", "--name", "file"}, func(string) string { return "" }, strings.NewReader(""), &bytes.Buffer{}, nil); err == nil || err.Error() != "AGENTUP_URL is not set; set it to the agent-up server URL" {
		t.Fatalf("unexpected environment error: %v", err)
	}
	if err := run([]string{"-"}, func(string) string { return "https://example.com" }, strings.NewReader(""), &bytes.Buffer{}, nil); err == nil || err.Error() != "stdin upload has no filename; provide one with --name FILENAME" {
		t.Fatalf("unexpected stdin name error: %v", err)
	}
}

func TestRunExplainsMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pdf")
	err := run([]string{missing}, func(string) string { return "https://example.com" }, strings.NewReader(""), &bytes.Buffer{}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), `cannot access upload path "`+missing+`":`) {
		t.Fatalf("unexpected missing path error: %v", err)
	}
}

func TestRunAddsUploadContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upload exceeds maximum size", http.StatusRequestEntityTooLarge)
	}))
	defer server.Close()
	err := run(
		[]string{"-", "--name", "large.bin"},
		func(string) string { return server.URL },
		strings.NewReader("content"),
		&bytes.Buffer{},
		server.Client(),
	)
	want := `failed to upload stdin as "large.bin": server rejected upload with HTTP 413 Request Entity Too Large: upload exceeds maximum size`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestRunRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	link := filepath.Join(directory, "link")
	if err := os.Symlink("missing", link); err != nil {
		t.Fatal(err)
	}
	err := run([]string{link}, func(string) string { return "https://example.com" }, strings.NewReader(""), &bytes.Buffer{}, nil)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	service, err := internalserver.New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(service.Handler())
	t.Cleanup(host.Close)
	return host
}
