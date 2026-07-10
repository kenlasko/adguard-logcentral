package adguard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/kenlasko/adguard-log-aggregator/internal/config"
)

// controlBase is the common path prefix for the AdGuard Home control API.
const controlBase = "/control"

// Client talks to a single AdGuard Home instance. It is safe for concurrent
// use because it holds no mutable state beyond the shared *http.Client.
type Client struct {
	name       string
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

// New builds a Client for one configured instance, sharing the given
// *http.Client (and thus its timeout and connection pool) across instances.
func New(instance config.Instance, httpClient *http.Client) *Client {
	return &Client{
		name:       instance.Name,
		baseURL:    strings.TrimRight(instance.URL, "/"),
		username:   instance.Username,
		password:   instance.Password,
		httpClient: httpClient,
	}
}

// Name returns the display name of the instance this client targets.
func (c *Client) Name() string { return c.name }

// get performs an authenticated GET against the control API and decodes the
// JSON body into out. A non-2xx response becomes a typed *APIError carrying the
// instance name and status code. The request honors ctx for cancellation.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	full := c.baseURL + controlBase + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return fmt.Errorf("adguard %q: build request: %w", c.name, err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("adguard %q: %s: %w", c.name, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Cap the error body so a misbehaving upstream cannot bloat logs.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &APIError{
			Instance: c.name,
			Path:     path,
			Status:   resp.StatusCode,
			Body:     strings.TrimSpace(string(body)),
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("adguard %q: %s: decode response: %w", c.name, path, err)
	}
	return nil
}
