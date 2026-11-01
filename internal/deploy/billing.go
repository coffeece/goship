package deploy

import "time"

// saoPaulo is the zone billing dates are shown in.
var saoPaulo = zoneSaoPaulo(time.LoadLocation)

// zoneSaoPaulo loads São Paulo's zone. Brazil has had no daylight saving time
// since 2019, so a machine without tzdata falls back to UTC-3.
func zoneSaoPaulo(load func(string) (*time.Location, error)) *time.Location {
	if loc, err := load("America/Sao_Paulo"); err == nil {
		return loc
	}
	return time.FixedZone("-03", -3*60*60)
}

// BillingNote says when billing starts for a resource an invited trial holds
// unbilled, or "" when it holds nothing (at is nil). A start already past is
// a trial that ended before GoShip moved the resource to its plan.
func BillingNote(at *time.Time, now time.Time) string {
	switch {
	case at == nil:
		return ""
	case at.After(now):
		return "Billing starts when the trial ends on " + at.In(saoPaulo).Format("2 Jan 2006") + "."
	default:
		return "Billing starts shortly."
	}
}
