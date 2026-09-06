package main

import (
	"strings"
	"testing"
)

func TestVolumeCommandsAreRegistered(t *testing.T) {
	var out, errOut strings.Builder
	m := buildManager(&out, &errOut)
	root := m.Cobra()
	for _, sub := range []string{"create", "list", "info", "bind", "unbind", "delete"} {
		c, _, err := root.Find([]string{"volume", sub})
		if err != nil || c == nil || c.Name() != sub || c.Parent() == nil || c.Parent().Name() != "volume" {
			t.Errorf("`coffeece volume %s` is not registered (err=%v)", sub, err)
		}
	}
}
