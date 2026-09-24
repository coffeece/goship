package portal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ShellOptions describes the terminal a shell is opened for.
type ShellOptions struct {
	Unit          string
	Isolated      bool
	Width, Height int
	Term          string
}

// Shell opens an interactive shell in one of the app's units. What is written
// to the result is typed into the terminal; what is read is its output.
func (c *Client) Shell(ctx context.Context, org, app string, opts ShellOptions) (io.ReadWriteCloser, error) {
	u, err := url.Parse(c.base + "/api/v1" + appPath(org, app) + "/shell")
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return nil, fmt.Errorf("the API address %q is neither http nor https", c.base)
	}
	q := url.Values{}
	if opts.Unit != "" {
		q.Set("unit", opts.Unit)
	}
	if opts.Isolated {
		q.Set("isolated", "true")
	}
	if opts.Width > 0 && opts.Height > 0 {
		q.Set("width", strconv.Itoa(opts.Width))
		q.Set("height", strconv.Itoa(opts.Height))
	}
	if opts.Term != "" {
		q.Set("term", opts.Term)
	}
	u.RawQuery = q.Encode()

	header := http.Header{}
	req := &http.Request{Header: header}
	c.decorate(req)

	dialer := websocket.Dialer{HandshakeTimeout: 20 * time.Second, Proxy: http.ProxyFromEnvironment}
	ws, resp, err := dialer.DialContext(ctx, u.String(), header)
	if c.trace != nil && resp != nil {
		fmt.Fprintf(c.trace, "GET %s → %s\n", u.Path, resp.Status)
	}
	if err != nil {
		if resp != nil {
			defer resp.Body.Close() //nolint:errcheck
			return nil, decodeError(resp)
		}
		return nil, err
	}
	return &shellConn{ws: ws}, nil
}

type shellConn struct {
	ws      *websocket.Conn
	pending io.Reader
	wmu     sync.Mutex
}

func (s *shellConn) Read(p []byte) (int, error) {
	for {
		if s.pending != nil {
			n, err := s.pending.Read(p)
			if err == io.EOF {
				s.pending = nil
				if n > 0 {
					return n, nil
				}
				continue
			}
			return n, err
		}
		// Reading is also what answers the API's keep-alive pings.
		_, r, err := s.ws.NextReader()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, net.ErrClosed) {
				return 0, io.EOF
			}
			return 0, err
		}
		s.pending = r
	}
}

func (s *shellConn) Write(p []byte) (int, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := s.ws.WriteMessage(websocket.TextMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *shellConn) Close() error {
	s.wmu.Lock()
	_ = s.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	s.wmu.Unlock()
	return s.ws.Close()
}
