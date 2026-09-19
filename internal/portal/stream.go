package portal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ErrStreamCut means an event stream ended without saying how the operation
// went. The API always closes a stream with a result, so this is a dropped
// connection, and what happened on the other side is unknown.
var ErrStreamCut = errors.New("the connection dropped before the result arrived")

// OperationError is an operation the API accepted, ran and saw fail: a build
// that does not compile, a command that exits non-zero.
type OperationError struct{ Message string }

func (e *OperationError) Error() string { return e.Message }

// event is one line of a GoShip event stream.
type event struct {
	Type  string `json:"type"`
	Data  string `json:"data"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	LogEntry
}

// LogEntry is one line of an app's output.
type LogEntry struct {
	Date    time.Time `json:"date"`
	Source  string    `json:"source"`
	Unit    string    `json:"unit"`
	Message string    `json:"message"`
}

// stream runs a request that answers with an event stream and feeds every
// event to on. It has no overall timeout: a build lasts as long as it lasts,
// bounded by ctx.
func (c *Client) stream(ctx context.Context, method, path, contentType string, body io.Reader, on func(event) error) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/x-ndjson")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.decorate(req)

	resp, err := c.streaming().Do(req)
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

	// A line is one event; build output can carry long lines.
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var ev event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return fmt.Errorf("unreadable event from the API: %w", err)
		}
		switch ev.Type {
		case "ping":
		case "result":
			if !ev.OK {
				return &OperationError{Message: ev.Error}
			}
			return nil
		default:
			if err := on(ev); err != nil {
				return err
			}
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("%w: %v", ErrStreamCut, err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrStreamCut
}

// streaming returns the HTTP client for long-lived responses: the configured
// one without its overall timeout.
func (c *Client) streaming() *http.Client {
	h := *c.http
	h.Timeout = 0
	return &h
}

func outputTo(out io.Writer) func(event) error {
	return func(ev event) error {
		if ev.Type != "output" {
			return nil
		}
		_, err := io.WriteString(out, ev.Data)
		return err
	}
}

// Deploy is one entry of an app's release history.
type Deploy struct {
	ID          string    `json:"id"`
	Version     int       `json:"version" table:"VERSION"`
	Origin      string    `json:"origin" table:"ORIGIN"`
	User        string    `json:"user" table:"BY"`
	Timestamp   time.Time `json:"timestamp" table:"WHEN"`
	Message     string    `json:"message" table:"MESSAGE"`
	Error       string    `json:"error,omitempty" table:"ERROR"`
	CanRollback bool      `json:"can_rollback" table:"ROLLBACK"`
	Image       string    `json:"image"`
	DurationMS  int64     `json:"duration_ms"`
}

func (c *Client) Deploys(ctx context.Context, org, app string, limit int) ([]Deploy, error) {
	var out []Deploy
	path := "/orgs/" + esc(org) + "/apps/" + esc(app) + "/deploys"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	return out, c.get(ctx, path, &out)
}

// DeployRequest is a source deploy. Archive is a gzipped tarball of the code.
type DeployRequest struct {
	Archive    io.Reader
	Message    string
	Dockerfile string
}

// Deploy uploads the archive and copies the build output to out. The body is
// produced while it is sent, so the archive is never held in memory.
func (c *Client) Deploy(ctx context.Context, org, app string, in DeployRequest, out io.Writer) error {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			if in.Message != "" {
				if err := mw.WriteField("message", in.Message); err != nil {
					return err
				}
			}
			if in.Dockerfile != "" {
				if err := mw.WriteField("dockerfile", in.Dockerfile); err != nil {
					return err
				}
			}
			// The API forwards the archive as it arrives, so it must come last.
			part, err := mw.CreateFormFile("file", "archive.tar.gz")
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, in.Archive); err != nil {
				return err
			}
			return mw.Close()
		}()
		pw.CloseWithError(err) //nolint:errcheck
	}()

	err := c.stream(ctx, http.MethodPost, "/orgs/"+esc(org)+"/apps/"+esc(app)+"/deploys", mw.FormDataContentType(), pr, outputTo(out))
	pr.CloseWithError(err) //nolint:errcheck
	return err
}

// Rollback releases a previous version again, e.g. "v12".
func (c *Client) Rollback(ctx context.Context, org, app, version string, out io.Writer) error {
	return c.streamJSON(ctx, "/orgs/"+esc(org)+"/apps/"+esc(app)+"/deploys/rollback", map[string]string{"version": version}, outputTo(out))
}

// Run runs a one-off command in the app's image.
func (c *Client) Run(ctx context.Context, org, app, command string, once, isolated bool, out io.Writer) error {
	body := map[string]any{"command": command, "once": once, "isolated": isolated}
	return c.streamJSON(ctx, "/orgs/"+esc(org)+"/apps/"+esc(app)+"/run", body, outputTo(out))
}

func (c *Client) streamJSON(ctx context.Context, path string, body any, on func(event) error) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.stream(ctx, http.MethodPost, path, "application/json", bytesReader(encoded), on)
}

// LogOptions selects which log lines to read.
type LogOptions struct {
	Lines  int
	Follow bool
	Source string
	Units  []string
}

// Logs reads the app's output, calling on for each line. With Follow it runs
// until ctx is cancelled.
func (c *Client) Logs(ctx context.Context, org, app string, opts LogOptions, on func(LogEntry) error) error {
	q := url.Values{}
	if opts.Lines > 0 {
		q.Set("lines", strconv.Itoa(opts.Lines))
	}
	if opts.Follow {
		q.Set("follow", "true")
	}
	if opts.Source != "" {
		q.Set("source", opts.Source)
	}
	for _, u := range opts.Units {
		q.Add("unit", u)
	}
	path := "/orgs/" + esc(org) + "/apps/" + esc(app) + "/logs"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return c.stream(ctx, http.MethodGet, path, "", nil, func(ev event) error {
		if ev.Type != "log" {
			return nil
		}
		return on(ev.LogEntry)
	})
}
