package portal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestErrorCodeFromAPIError(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		fmt.Fprintln(w, `{"error":"your trial covers up to 2 apps","code":"trial_allowance_exceeded"}`)
	})
	_, err := c.Orgs(context.Background())
	if got := ErrorCode(err); got != "trial_allowance_exceeded" {
		t.Errorf("ErrorCode = %q, want trial_allowance_exceeded (err %v)", got, err)
	}
	if got := ErrorCode(fmt.Errorf("listing: %w", err)); got != "trial_allowance_exceeded" {
		t.Errorf("wrapped: ErrorCode = %q", got)
	}
}

func TestErrorCodeFromStreamResult(t *testing.T) {
	c := streamServer(t, blogPath+"/deploys/rollback", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"result","ok":false,"error":"deploy failed: x","code":"deploy_failed"}`)
	})
	err := c.Rollback(context.Background(), "acme", "atlas", "staging", "blog", "v3", printOutput(new(discard)))
	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.Code != "deploy_failed" || opErr.Message != "deploy failed: x" {
		t.Fatalf("error = %#v, want the code and the message", err)
	}
	if got := ErrorCode(err); got != "deploy_failed" {
		t.Errorf("ErrorCode = %q", got)
	}
}

func TestErrorCodeEmptyForLocalErrors(t *testing.T) {
	for _, err := range []error{nil, errors.New("x"), ErrStreamCut, fmt.Errorf("x: %w", ErrStreamCut)} {
		if got := ErrorCode(err); got != "" {
			t.Errorf("ErrorCode(%v) = %q, want empty", err, got)
		}
	}
}

// An API from before codes answers without one: nothing to report.
func TestErrorCodeOldAPIWithoutCode(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":"not found"}`)
	})
	_, err := c.Orgs(context.Background())
	if err == nil || ErrorCode(err) != "" {
		t.Errorf("err %v, ErrorCode %q, want an error without a code", err, ErrorCode(err))
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
