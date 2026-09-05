package main

import (
	"testing"

	goVersion "github.com/hashicorp/go-version"
)

// TestProtocolVersionSatisfiesTheAPIFloor pins the reason protocolVersion
// exists: the Tsuru API refuses clients below Supported-Tsuru (1.0.1) with a
// banner telling users to download the tsuru client. coffeece's release
// numbers are 0.x, so the release number must never be what the API compares.
func TestProtocolVersionSatisfiesTheAPIFloor(t *testing.T) {
	floor := goVersion.Must(goVersion.NewVersion("1.0.1"))
	got, err := goVersion.NewVersion(protocolVersion)
	if err != nil {
		t.Fatalf("protocolVersion %q is not a version: %v", protocolVersion, err)
	}
	if got.Compare(floor) < 0 {
		t.Fatalf("protocolVersion %s is below the API floor %s", got, floor)
	}
	if rel, err := goVersion.NewVersion("0.1.0"); err == nil && rel.Compare(floor) >= 0 {
		t.Fatalf("a 0.x release number would pass the floor; this test's premise is wrong")
	}
}
