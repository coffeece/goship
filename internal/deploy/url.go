package deploy

import (
	"strings"

	"github.com/coffeece/goship/internal/portal"
)

// PublicURL is the address to show after a deploy: a custom domain when the
// app has one, otherwise the platform address it always has.
func PublicURL(a *portal.App) string {
	if len(a.CNames) > 0 {
		return "https://" + strings.TrimPrefix(strings.TrimPrefix(a.CNames[0], "https://"), "http://")
	}
	if len(a.Addresses) > 0 {
		// The platform reports its address without a scheme; every app is served over https.
		if !strings.Contains(a.Addresses[0], "://") {
			return "https://" + a.Addresses[0]
		}
		return a.Addresses[0]
	}
	return ""
}
