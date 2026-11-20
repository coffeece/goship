package mcptools

import "testing"

// The tool returns the end of a log, where a failure is, and says when it
// left the beginning out.
func TestLastLinesKeepsTheEndOfTheLog(t *testing.T) {
	log := "one\ntwo\nthree\nfour\n"
	if got, cut := lastLines(log, 2); got != "three\nfour\n" || !cut {
		t.Errorf("last 2 = %q, cut %v", got, cut)
	}
	if got, cut := lastLines(log, 10); got != log || cut {
		t.Errorf("whole log = %q, cut %v", got, cut)
	}
	if got, cut := lastLines("no newline at the end", 0); got != "no newline at the end" || cut {
		t.Errorf("default = %q, cut %v", got, cut)
	}
}
