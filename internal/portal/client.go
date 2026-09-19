package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	base    string
	token   string
	version string
	http    *http.Client
	trace   io.Writer
}

type Option func(*Client)

// WithTrace logs one line per request, for --verbose.
func WithTrace(w io.Writer) Option { return func(c *Client) { c.trace = w } }

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithVersion reports the CLI's version to the API on every request.
func WithVersion(v string) Option { return func(c *Client) { c.version = v } }

func New(base, token string, opts ...Option) *Client {
	c := &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Error is a non-2xx response from the API.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("goship api: %s", http.StatusText(e.Status))
	}
	return e.Message
}

func IsNotFound(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

func IsUnauthorized(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized
}

func IsForbidden(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) put(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPut, path, body, out)
}

func (c *Client) delete(ctx context.Context, path string, body any) error {
	return c.do(ctx, http.MethodDelete, path, body, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.decorate(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if c.trace != nil {
		fmt.Fprintf(c.trace, "%s %s → %s\n", method, req.URL.Path, resp.Status)
	}

	if resp.StatusCode >= 400 {
		return decodeError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// decorate adds what every request carries: the credential, and the CLI's
// version so the API can tell an outdated binary to upgrade.
func (c *Client) decorate(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.version != "" {
		req.Header.Set("User-Agent", "goship/"+c.version)
		req.Header.Set("X-Goship-Version", c.version)
	}
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func decodeError(resp *http.Response) error {
	var payload struct {
		Error string `json:"error"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = json.Unmarshal(data, &payload)

	if payload.Error == "" && len(data) > 0 && len(data) < 200 {
		payload.Error = strings.TrimSpace(string(data))
	}
	return &Error{Status: resp.StatusCode, Message: payload.Error}
}

func esc(s string) string { return url.PathEscape(s) }
