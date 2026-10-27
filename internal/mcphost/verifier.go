package mcphost

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInvalidToken is a token that is not one the issuer signed, not for
	// now, or not readable: the caller must sign in again.
	ErrInvalidToken = errors.New("invalid token")
	// ErrUnavailable is a token that could not be checked, because the
	// issuer's keys could not be fetched. Signing in again would not help.
	ErrUnavailable = errors.New("signing keys unavailable")
	// ErrExpired is an invalid token that was good until it expired: the
	// client should refresh it.
	ErrExpired = fmt.Errorf("%w: expired", ErrInvalidToken)
)

// leeway absorbs clock drift between the portal and this server.
const leeway = 30 * time.Second

// refetchEvery bounds how often a token signed with an unknown key makes the
// verifier fetch the key set again, so a stream of junk tokens cannot turn it
// into a load generator against the issuer.
const refetchEvery = time.Minute

// Claims is what a verified token says about its holder.
type Claims struct {
	Subject string
	Email   string
	Expiry  time.Time
}

// Verifier checks the portal's RS256 access tokens against the issuer's
// published key set.
type Verifier struct {
	issuer  string
	jwksURL string
	http    *http.Client
	now     func() time.Time

	fetchMu sync.Mutex // one fetch at a time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

// NewVerifier trusts tokens from issuer signed by a key in jwksURL.
func NewVerifier(issuer, jwksURL string) *Verifier {
	return &Verifier{
		issuer:  issuer,
		jwksURL: jwksURL,
		http:    &http.Client{Timeout: 10 * time.Second},
		now:     time.Now,
	}
}

// Verify checks token and returns its claims. The error wraps ErrInvalidToken
// or ErrUnavailable.
func (v *Verifier) Verify(ctx context.Context, token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: not a JWT", ErrInvalidToken)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return nil, fmt.Errorf("%w: header: %v", ErrInvalidToken, err) //nolint:errorlint // the cause is detail, not something to match
	}
	// Only the algorithm the portal signs with: "none" or an HMAC algorithm
	// keyed with the public key are the classic ways to forge a JWT.
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("%w: algorithm %q", ErrInvalidToken, header.Alg)
	}
	key, err := v.key(ctx, header.Kid)
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: signature encoding", ErrInvalidToken)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return nil, fmt.Errorf("%w: bad signature", ErrInvalidToken)
	}

	var claims struct {
		Iss   string   `json:"iss"`
		Sub   string   `json:"sub"`
		Email string   `json:"email"`
		Exp   *float64 `json:"exp"`
		Nbf   *float64 `json:"nbf"`
	}
	if err := decodeSegment(parts[1], &claims); err != nil {
		return nil, fmt.Errorf("%w: claims: %v", ErrInvalidToken, err) //nolint:errorlint // the cause is detail, not something to match
	}
	if claims.Iss != v.issuer {
		return nil, fmt.Errorf("%w: issuer %q", ErrInvalidToken, claims.Iss)
	}
	now := v.now()
	if claims.Exp == nil {
		return nil, fmt.Errorf("%w: no expiry", ErrInvalidToken)
	}
	exp := unixTime(*claims.Exp)
	if now.After(exp.Add(leeway)) {
		return nil, ErrExpired
	}
	if claims.Nbf != nil && now.Add(leeway).Before(unixTime(*claims.Nbf)) {
		return nil, fmt.Errorf("%w: not valid yet", ErrInvalidToken)
	}
	return &Claims{Subject: claims.Sub, Email: claims.Email, Expiry: exp}, nil
}

// key returns the public key kid names, fetching the key set when it is not
// known yet. A token without a kid is accepted only while the issuer
// publishes a single key.
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if k, ok := v.cached(kid); ok {
		return k, nil
	}
	v.fetchMu.Lock()
	defer v.fetchMu.Unlock()
	// Another request may have fetched while this one waited.
	if k, ok := v.cached(kid); ok {
		return k, nil
	}
	v.mu.Lock()
	recent := !v.fetched.IsZero() && v.now().Sub(v.fetched) < refetchEvery
	haveKeys := len(v.keys) > 0
	v.mu.Unlock()
	if recent {
		return nil, fmt.Errorf("%w: unknown signing key %q", ErrInvalidToken, kid)
	}
	keys, err := v.fetch(ctx)
	if err != nil {
		if haveKeys {
			// The cached set still stands; this kid is just not in it.
			return nil, fmt.Errorf("%w: unknown signing key %q", ErrInvalidToken, kid)
		}
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err) //nolint:errorlint // ErrUnavailable is what callers match
	}
	v.mu.Lock()
	v.keys, v.fetched = keys, v.now()
	v.mu.Unlock()
	if k, ok := v.cached(kid); ok {
		return k, nil
	}
	return nil, fmt.Errorf("%w: unknown signing key %q", ErrInvalidToken, kid)
}

func (v *Verifier) cached(kid string) (*rsa.PublicKey, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if kid == "" {
		if len(v.keys) == 1 {
			for _, k := range v.keys {
				return k, true
			}
		}
		return nil, false
	}
	k, ok := v.keys[kid]
	return k, ok
}

func (v *Verifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil) //nolint:gosec // G704: the URL is the configured issuer's
	if err != nil {
		return nil, err
	}
	resp, err := v.http.Do(req) //nolint:gosec // G704: as above
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", v.jwksURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", v.jwksURL, resp.Status)
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&set); err != nil {
		return nil, fmt.Errorf("reading %s: %w", v.jwksURL, err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%s has no RS256 signing key", v.jwksURL)
	}
	return keys, nil
}

func decodeSegment(s string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func unixTime(f float64) time.Time {
	sec := int64(f)
	return time.Unix(sec, int64((f-float64(sec))*1e9))
}
