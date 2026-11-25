package portal

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func tlsStorage(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	t.Cleanup(SetStorageTLSForTest(srv.Client().Transport.(*http.Transport).TLSClientConfig))
	return srv
}

func TestSignedLinkSendsNoCredentials(t *testing.T) {
	var got http.Header
	srv := tlsStorage(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte("dump-bytes")) //nolint:errcheck
	})
	var buf bytes.Buffer
	n, err := FetchSignedLink(context.Background(), srv.URL+"/b/1?sig=secret", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 10 || buf.String() != "dump-bytes" {
		t.Errorf("n = %d, body = %q", n, buf.String())
	}
	for _, h := range []string{"Authorization", "X-Goship-Version", "Cookie"} {
		if got.Get(h) != "" {
			t.Errorf("%s sent to the storage host", h)
		}
	}
}

func TestSignedLinkRefusesPlainHTTP(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	var buf bytes.Buffer
	_, err := FetchSignedLink(context.Background(), srv.URL+"/b?sig=secret", &buf)
	if err == nil || hit || buf.Len() != 0 {
		t.Fatalf("err = %v, contacted = %v, written = %d", err, hit, buf.Len())
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error leaks the link: %v", err)
	}
}

func TestSignedLinkRefusesRedirectToPlainHTTP(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("plain http target was contacted")
	}))
	defer plain.Close()
	srv := tlsStorage(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/x?sig=other", http.StatusFound)
	})
	var buf bytes.Buffer
	_, err := FetchSignedLink(context.Background(), srv.URL+"/b?sig=secret", &buf)
	if err == nil || buf.Len() != 0 {
		t.Fatalf("err = %v, written = %d", err, buf.Len())
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "other") {
		t.Errorf("error leaks a link: %v", err)
	}
}

func TestSignedLinkForbiddenNamesNeitherLinkNorBody(t *testing.T) {
	srv := tlsStorage(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("<Error>signature for secret mismatch</Error>")) //nolint:errcheck
	})
	var buf bytes.Buffer
	_, err := FetchSignedLink(context.Background(), srv.URL+"/b?sig=secret", &buf)
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "403") || strings.Contains(msg, "secret") || strings.Contains(msg, "Error>") || strings.Contains(msg, srv.URL) {
		t.Errorf("error = %q", msg)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes of an error page", buf.Len())
	}
}

func TestSignedLinkConnectionFailureDoesNotLeakTheLink(t *testing.T) {
	srv := tlsStorage(t, func(http.ResponseWriter, *http.Request) {})
	link := srv.URL + "/private/path?sig=secret"
	srv.Close()
	var buf bytes.Buffer
	_, err := FetchSignedLink(context.Background(), link, &buf)
	if err == nil {
		t.Fatal("expected an error")
	}
	host := strings.TrimPrefix(srv.URL, "https://")
	for _, leak := range []string{"secret", "/private/path", host, "127.0.0.1"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q contains %q", err, leak)
		}
	}
}

func TestSignedLinkShortBodyIsAFailure(t *testing.T) {
	srv := tlsStorage(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("short")) //nolint:errcheck
	})
	var buf bytes.Buffer
	if _, err := FetchSignedLink(context.Background(), srv.URL+"/b", &buf); err == nil {
		t.Fatal("a truncated transfer must fail")
	}
}
