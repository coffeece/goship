package mcphost

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testIssuer = "https://goship.example"

type signer struct {
	key *rsa.PrivateKey
	kid string
}

func newSigner(t *testing.T, kid string) signer {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return signer{key: k, kid: kid}
}

func b64json(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// sign makes a token the way the portal does, with claims overriding the
// defaults; a nil value deletes a claim.
func (s signer) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	c := map[string]any{
		"iss": testIssuer, "sub": "ada@example.com", "email": "ada@example.com",
		"groups": []string{"acme"}, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "jti": "1",
	}
	for k, v := range claims {
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
	}
	signing := b64json(t, map[string]string{"alg": "RS256", "typ": "JWT", "kid": s.kid}) + "." + b64json(t, c)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (s signer) jwk() map[string]string {
	return map[string]string{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": s.kid,
		"n": base64.RawURLEncoding.EncodeToString(s.key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(s.key.E)).Bytes()),
	}
}

// jwksServer publishes the signers in *keys and counts the fetches.
func jwksServer(t *testing.T, keys *atomic.Pointer[[]signer], fetches *atomic.Int32) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		var set []map[string]string
		for _, s := range *keys.Load() {
			set = append(set, s.jwk())
		}
		json.NewEncoder(w).Encode(map[string]any{"keys": set})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func newTestVerifier(t *testing.T, signers ...signer) (*Verifier, *atomic.Pointer[[]signer], *atomic.Int32) {
	t.Helper()
	var keys atomic.Pointer[[]signer]
	keys.Store(&signers)
	var fetches atomic.Int32
	return NewVerifier(testIssuer, jwksServer(t, &keys, &fetches)), &keys, &fetches
}

func TestVerifierAcceptsAPortalToken(t *testing.T) {
	s := newSigner(t, "k1")
	v, _, _ := newTestVerifier(t, s)
	c, err := v.Verify(context.Background(), s.sign(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "ada@example.com" || c.Email != "ada@example.com" || c.Expiry.Before(time.Now()) {
		t.Errorf("claims %+v", c)
	}
}

func TestVerifierRefusesBadTokens(t *testing.T) {
	s := newSigner(t, "k1")
	other := newSigner(t, "k1") // same kid, different key: a forgery
	v, _, _ := newTestVerifier(t, s)

	good := s.sign(t, nil)
	parts := strings.Split(good, ".")
	none := b64json(t, map[string]string{"alg": "none", "kid": "k1"}) + "." + parts[1] + "."
	hs := b64json(t, map[string]string{"alg": "HS256", "kid": "k1"}) + "." + parts[1] + "." + parts[2]

	for name, tc := range map[string]struct {
		token string
		want  error
	}{
		"expired":        {s.sign(t, map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}), ErrExpired},
		"no expiry":      {s.sign(t, map[string]any{"exp": nil}), ErrInvalidToken},
		"not yet valid":  {s.sign(t, map[string]any{"nbf": time.Now().Add(time.Hour).Unix()}), ErrInvalidToken},
		"wrong issuer":   {s.sign(t, map[string]any{"iss": "https://evil.example"}), ErrInvalidToken},
		"alg none":       {none, ErrInvalidToken},
		"alg HS256":      {hs, ErrInvalidToken},
		"bad signature":  {other.sign(t, nil), ErrInvalidToken},
		"tampered":       {parts[0] + "." + b64json(t, map[string]any{"iss": testIssuer, "sub": "eve", "exp": time.Now().Add(time.Hour).Unix()}) + "." + parts[2], ErrInvalidToken},
		"not a JWT":      {"gsp_abcdef", ErrInvalidToken},
		"garbled header": {"!!!." + parts[1] + "." + parts[2], ErrInvalidToken},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), tc.token)
			if !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVerifierToleratesClockSkew(t *testing.T) {
	s := newSigner(t, "k1")
	v, _, _ := newTestVerifier(t, s)
	if _, err := v.Verify(context.Background(), s.sign(t, map[string]any{"exp": time.Now().Add(-10 * time.Second).Unix()})); err != nil {
		t.Errorf("a token 10s past expiry should pass: %v", err)
	}
}

func TestVerifierFetchesANewKeyOnceAMinute(t *testing.T) {
	old, rotated := newSigner(t, "old"), newSigner(t, "new")
	v, keys, fetches := newTestVerifier(t, old)
	now := time.Now()
	v.now = func() time.Time { return now }

	if _, err := v.Verify(context.Background(), old.sign(t, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), old.sign(t, nil)); err != nil || fetches.Load() != 1 {
		t.Fatalf("a known key must not refetch: err %v, fetches %d", err, fetches.Load())
	}

	// The portal rotated its key; a token under it is unknown until a refetch
	// is allowed.
	keys.Store(&[]signer{rotated})
	if _, err := v.Verify(context.Background(), rotated.sign(t, nil)); !errors.Is(err, ErrInvalidToken) || fetches.Load() != 1 {
		t.Fatalf("within a minute of a fetch: err %v, fetches %d", err, fetches.Load())
	}
	now = now.Add(2 * time.Minute)
	if _, err := v.Verify(context.Background(), rotated.sign(t, nil)); err != nil || fetches.Load() != 2 {
		t.Fatalf("after a minute the new key is fetched: err %v, fetches %d", err, fetches.Load())
	}
	// Unknown kids are rate limited too.
	junk := newSigner(t, "junk").sign(t, nil)
	for range 5 {
		v.Verify(context.Background(), junk) //nolint:errcheck
	}
	if fetches.Load() != 2 {
		t.Errorf("junk kids caused %d fetches", fetches.Load()-2)
	}
}

func TestVerifierSaysWhenItCannotFetchKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()
	v := NewVerifier(testIssuer, srv.URL)
	_, err := v.Verify(context.Background(), newSigner(t, "k1").sign(t, nil))
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrInvalidToken) {
		t.Errorf("got %v, want ErrUnavailable only", err)
	}
}
