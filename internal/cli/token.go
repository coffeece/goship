package cli

import "os"

// resolveToken picks the credential to send: GOSHIP_TOKEN, for CI and anywhere
// without a browser, wins over whatever `goship login` stored.
func resolveToken(stored string) string {
	if env := os.Getenv("GOSHIP_TOKEN"); env != "" {
		return env
	}
	return stored
}
