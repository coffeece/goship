package deploy

import (
	"errors"
	"testing"

	"github.com/coffeece/goship/internal/portal"
)

func TestResizeReconnectHint(t *testing.T) {
	reconnect := &portal.Error{Status: 409, Message: "Reconnect your account.", Code: "reconnect_required"}
	got := ResizeReconnectHint("https://goship.sh", "acme", "c1", reconnect)
	want := "Reconnect your account. Reconnect it from the dashboard: https://goship.sh/orgs/acme/clusters/c1/resize"
	if got == nil || got.Error() != want {
		t.Errorf("got %v, want %q", got, want)
	}

	other := &portal.Error{Status: 409, Message: "busy"}
	if got := ResizeReconnectHint("https://goship.sh", "a", "c", other); !errors.Is(got, other) {
		t.Errorf("other error changed: %v", got)
	}
	plain := errors.New("boom")
	if got := ResizeReconnectHint("https://goship.sh", "a", "c", plain); !errors.Is(got, plain) {
		t.Errorf("plain error changed: %v", got)
	}
	if got := ResizeReconnectHint("https://goship.sh", "a", "c", nil); got != nil {
		t.Errorf("nil changed: %v", got)
	}
}

func TestResizeReconnectHintKeepsTheAPIError(t *testing.T) {
	apiErr := &portal.Error{Status: 409, Message: "reconnect the DigitalOcean account to resize.", Code: "reconnect_required"}
	err := ResizeReconnectHint("https://goship.sh", "acme", "c-home", apiErr)
	if portal.ErrorCode(err) != "reconnect_required" {
		t.Errorf("ErrorCode = %q, want reconnect_required", portal.ErrorCode(err))
	}
	if want := "reconnect the DigitalOcean account to resize. Reconnect it from the dashboard: https://goship.sh/orgs/acme/clusters/c-home/resize"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}
