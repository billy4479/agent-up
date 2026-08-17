package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRejectsInvalidURLs(t *testing.T) {
	for _, test := range []struct {
		rawURL string
		want   string
	}{
		{rawURL: "", want: "AGENTUP_URL is empty"},
		{rawURL: "localhost:8080", want: `AGENTUP_URL uses unsupported scheme "localhost"`},
		{rawURL: "ftp://example.com", want: `AGENTUP_URL uses unsupported scheme "ftp"`},
		{rawURL: "https://example.com?query=yes", want: "AGENTUP_URL contains a query string"},
		{rawURL: "https://example.com/#fragment", want: "AGENTUP_URL contains a fragment"},
	} {
		t.Run(test.rawURL, func(t *testing.T) {
			_, err := New(test.rawURL, nil)
			if err == nil || !strings.HasPrefix(err.Error(), test.want) {
				t.Fatalf("New(%q) error = %v", test.rawURL, err)
			}
		})
	}
}

func TestUploadErrorsExplainServerFailure(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		want    string
	}{
		{
			name: "rejected",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "upload exceeds maximum size", http.StatusRequestEntityTooLarge)
			}),
			want: "server rejected upload with HTTP 413 Request Entity Too Large: upload exceeds maximum size",
		},
		{
			name: "invalid JSON",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("not JSON"))
			}),
			want: "server accepted upload but returned invalid JSON:",
		},
		{
			name: "missing slug",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{}`))
			}),
			want: "server accepted upload but returned no slug",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			client, err := New(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.UploadFile("file", "", strings.NewReader("content"))
			if err == nil || !strings.HasPrefix(err.Error(), test.want) {
				t.Fatalf("error = %q, want prefix %q", err, test.want)
			}
		})
	}
}

func TestUploadErrorExplainsConnectionFailure(t *testing.T) {
	httpClient := &http.Client{Transport: failingTransport{}}
	client, err := New("https://agent-up.example", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.UploadFile("file", "", strings.NewReader("content"))
	if err == nil || !strings.Contains(err.Error(), `cannot reach agent-up server "https://agent-up.example"`) || !strings.Contains(err.Error(), "connection failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection failed")
}

func TestUploadFileConstructsEscapedPublicURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prefix/api/uploads" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("name"); got != "a file.txt" {
			t.Errorf("name = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "plain/text" {
			t.Errorf("Content-Type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "content" {
			t.Errorf("body = %q", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"slug":"abcdefgh"}`))
	}))
	defer server.Close()

	client, err := New(server.URL+"/prefix/", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	publicURL, err := client.UploadFile("a file.txt", "plain/text", strings.NewReader("content"))
	if err != nil {
		t.Fatal(err)
	}
	if publicURL != server.URL+"/prefix/abcdefgh/a%20file.txt" {
		t.Fatalf("URL = %q", publicURL)
	}
}

func TestIndexURLDoesNotIncludeFilename(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"slug":"abcdefgh"}`))
	}))
	defer server.Close()
	client, err := New(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	publicURL, err := client.UploadFile("index.html", "", strings.NewReader("site"))
	if err != nil {
		t.Fatal(err)
	}
	if publicURL != server.URL+"/abcdefgh" {
		t.Fatalf("URL = %q", publicURL)
	}
}
