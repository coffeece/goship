package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const minSizeLimitMB = 10

// parseSizeMB reads a database size limit: "none" clears it (nil); otherwise
// plain MB, or a number with an MB or GB suffix, in any case.
func parseSizeMB(s string) (*int, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "none" {
		return nil, nil
	}
	unit := 1.0
	switch {
	case strings.HasSuffix(v, "gb"):
		unit, v = 1024, strings.TrimSuffix(v, "gb")
	case strings.HasSuffix(v, "mb"):
		v = strings.TrimSuffix(v, "mb")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return nil, fmt.Errorf("invalid size %q: use a number of MB, a value like 500MB or 2GB, or none", s)
	}
	mb := int(math.Round(n * unit))
	if mb < minSizeLimitMB {
		return nil, fmt.Errorf("size limit %q is too small: the minimum is %d MB", s, minSizeLimitMB)
	}
	return &mb, nil
}

// parseDiskGB reads a disk cap in GB: "none" or 0 is 0 (no cap), otherwise a whole
// number with an optional GB suffix.
func parseDiskGB(s string) (int, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "none" || v == "0" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(v, "gb")))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid disk cap %q: use a whole number of GB, like 100 or 100GB, or none (or 0) for no cap", s)
	}
	return n, nil
}
