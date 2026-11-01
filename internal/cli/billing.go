package cli

import (
	"time"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
)

// reportChange reports a create or a plan change. While an invited trial
// holds the resource unbilled, the line is followed by when billing starts,
// and the JSON document carries billing_starts_at.
func reportChange(app *App, ch portal.Change, format string, args ...any) error {
	var extra map[string]any
	if ch.BillingStartsAt != nil {
		extra = map[string]any{"billing_starts_at": ch.BillingStartsAt}
	}
	if err := app.Renderer().MessageWith(extra, format, args...); err != nil {
		return err
	}
	return printBilling(app, ch.BillingStartsAt)
}

// printBilling says when billing starts, for people: -o json already has the
// date in the document it prints.
func printBilling(app *App, at *time.Time) error {
	note := deploy.BillingNote(at, time.Now())
	if note == "" || app.Global.Output == render.JSON {
		return nil
	}
	return app.Renderer().Message("%s", note)
}
