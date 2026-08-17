package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	internalarchive "github.com/billy4479/agent-up/internal/archive"
)

type Client struct {
	base       *url.URL
	httpClient *http.Client
}

type uploadResponse struct {
	Slug string `json:"slug"`
}

func New(rawURL string, httpClient *http.Client) (*Client, error) {
	rawURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if rawURL == "" {
		return nil, fmt.Errorf("AGENTUP_URL is empty; set it to an HTTP or HTTPS server URL")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("AGENTUP_URL is not a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("AGENTUP_URL uses unsupported scheme %q; use http or https", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("AGENTUP_URL has no hostname; set it to the full server URL")
	}
	if parsed.RawQuery != "" {
		return nil, fmt.Errorf("AGENTUP_URL contains a query string; remove the query string")
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("AGENTUP_URL contains a fragment; remove the fragment")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{base: parsed, httpClient: httpClient}, nil
}

func (c *Client) UploadFile(name, mimeType string, body io.Reader) (string, error) {
	// Keep ownership with the caller even when body also implements io.Closer.
	slug, err := c.upload("file", name, mimeType, struct{ io.Reader }{body})
	if err != nil {
		return "", err
	}
	if name == "index.html" {
		return c.publicURL(slug), nil
	}
	return c.publicURL(slug, name), nil
}

func (c *Client) UploadDirectory(directory string) (string, error) {
	reader, writer := io.Pipe()
	go func() {
		writer.CloseWithError(internalarchive.WriteDirectory(writer, directory))
	}()
	slug, err := c.upload("directory", "", "application/x-tar", reader)
	if err != nil {
		return "", err
	}
	return c.publicURL(slug), nil
}

func (c *Client) upload(kind, name, mimeType string, body io.Reader) (string, error) {
	endpoint := *c.base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/uploads"
	query := endpoint.Query()
	query.Set("kind", kind)
	if name != "" {
		query.Set("name", name)
	}
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequest(http.MethodPost, endpoint.String(), body)
	if err != nil {
		return "", fmt.Errorf("cannot create upload request: %w", err)
	}
	if mimeType != "" {
		request.Header.Set("Content-Type", mimeType)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("cannot reach agent-up server %q: %w", c.base.String(), err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		detail := strings.Join(strings.Fields(string(message)), " ")
		if detail == "" {
			detail = "the server did not provide an error message"
		}
		return "", fmt.Errorf("server rejected upload with HTTP %s: %s", response.Status, detail)
	}
	var result uploadResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("server accepted upload but returned invalid JSON: %w", err)
	}
	if result.Slug == "" {
		return "", fmt.Errorf("server accepted upload but returned no slug")
	}
	return result.Slug, nil
}

func (c *Client) publicURL(parts ...string) string {
	result := *c.base
	result.Path = strings.TrimRight(result.Path, "/") + "/" + strings.Join(parts, "/")
	return result.String()
}
