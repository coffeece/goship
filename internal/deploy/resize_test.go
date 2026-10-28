package deploy

import (
	"errors"
	"testing"

	"github.com/coffeece/goship/internal/portal"
)

func TestResizeReconnectHint(t *testing.T) {
	reconnect := &portal.Error{Status: 409, Message: "Reconnect your account.", Code: "reconnect_required"}
	got := ResizeReconnectHint("https://goship.sh", "acme", "web", "n1", reconnect)
	want := "Reconnect your account. Reconnect it from the dashboard: https://goship.sh/orgs/acme/projects/web/nodes/n1/resize"
	if got == nil || got.Error() != want {
		t.Errorf("got %v, want %q", got, want)
	}

	other := &portal.Error{Status: 409, Message: "busy"}
	if got := ResizeReconnectHint("https://goship.sh", "a", "p", "n", other); !errors.Is(got, other) {
		t.Errorf("other error changed: %v", got)
	}
	plain := errors.New("boom")
	if got := ResizeReconnectHint("https://goship.sh", "a", "p", "n", plain); !errors.Is(got, plain) {
		t.Errorf("plain error changed: %v", got)
	}
	if got := ResizeReconnectHint("https://goship.sh", "a", "p", "n", nil); got != nil {
		t.Errorf("nil changed: %v", got)
	}
}
