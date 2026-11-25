package portal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const maxStorageRedirects = 5

var (
	errPlainHTTP       = errors.New("the download link is not https; refusing to use it")
	errTooManyRedirect = errors.New("the download link redirected too many times")
)

// storageClient fetches signed links. It is not the API client: it never
// carries the token or the version header, and it has no total timeout because
// a dump takes as long as it takes.
var storageClient = newStorageClient(nil)

func newStorageClient(tlsConfig *tls.Config) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSClientConfig:       tlsConfig,
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			DisableCompression:    true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errPlainHTTP
			}
			if len(via) > maxStorageRedirects {
				return errTooManyRedirect
			}
			return nil
		},
	}
}

// SetStorageTLSForTest makes FetchSignedLink trust a test server's
// certificate. It returns the function that puts the default back.
func SetStorageTLSForTest(c *tls.Config) (restore func()) {
	prev := storageClient
	storageClient = newStorageClient(c)
	return func() { storageClient = prev }
}

// FetchSignedLink copies what the signed link serves into w and returns the
// byte count. Errors never contain the link: it is a credential, and Go's
// transport errors quote it.
func FetchSignedLink(ctx context.Context, link string, w io.Writer) (int64, error) {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return 0, errors.New("the download link is not a valid URL")
	}
	if u.Scheme != "https" {
		return 0, errPlainHTTP
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, errors.New("the download link is not a valid URL")
	}
	resp, err := storageClient.Do(req)
	if err != nil {
		return 0, storageError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("the storage host answered %s; the link may have expired, ask for a new one", resp.Status)
	}

	cw := &errWriter{w: w}
	n, err := io.Copy(cw, resp.Body)
	if cw.err != nil {
		return n, cw.err
	}
	if err != nil {
		return n, storageError(ctx, err)
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return n, fmt.Errorf("the transfer stopped early: got %d of %d bytes", n, resp.ContentLength)
	}
	return n, nil
}

// errWriter remembers a write failure so it is told apart from a read failure.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	n, err := e.w.Write(p)
	if err != nil {
		e.err = err
	}
	return n, err
}

// storageError words a transport failure without the URL (and so the
// signature) that *url.Error quotes, nor the host in a DNS or dial error.
func storageError(ctx context.Context, err error) error {
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, errPlainHTTP):
		return errPlainHTTP
	case errors.Is(err, errTooManyRedirect):
		return errTooManyRedirect
	}
	return errors.New("could not download from the storage host: the connection failed or was interrupted")
}
