package cli

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/coffeece/goship/internal/tsuru"
)

// resolveToken picks the credential to send. There are two stores and only one
// of them stays fresh: tsuru-client rewrites its own on every command, while a
// copy saved into our config at login time silently goes stale. So prefer
// whichever token is still valid rather than a fixed order, and treat the
// config copy as the home for `goship login --email`, which has no other.
func resolveToken(stored string) string {
	if env := os.Getenv("GOSHIP_TOKEN"); env != "" {
		return env
	}

	platform, err := tsuru.Token()
	if err != nil {
		platform = ""
	}
	switch {
	case platform != "" && !expired(platform):
		return platform
	case stored != "" && !expired(stored):
		return stored
	case platform != "":
		return platform
	default:
		return stored
	}
}

// expired reports whether a JWT is past its exp claim. Anything unparseable is
// treated as live: the server is the authority, and guessing "expired" here
// would refuse a perfectly good opaque token.
func expired(token string) bool {
	exp, ok := expiry(token)
	return ok && time.Now().After(exp)
}

func expiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}
