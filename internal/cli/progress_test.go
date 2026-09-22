package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coffeece/goship/internal/portal"
)

type testClock struct{ t time.Time }

func (c *testClock) now() time.Time          { return c.t }
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestProgress(mode progressMode) (*progress, *strings.Builder, *testClock) {
	out := &strings.Builder{}
	clk := &testClock{t: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)}
	p := &progress{out: out, mode: mode, width: 60, now: clk.now}
	p.began = clk.now()
	return p, out, clk
}

func send(t *testing.T, p *progress, events ...portal.ReleaseEvent) {
	t.Helper()
	for _, ev := range events {
		if err := p.Event(ev); err != nil {
			t.Fatal(err)
		}
	}
}

func stepEv(step, state, detail string) portal.ReleaseEvent {
	return portal.ReleaseEvent{Type: "step", Step: step, State: state, Detail: detail}
}

func outputEv(step, data string) portal.ReleaseEvent {
	return portal.ReleaseEvent{Type: "output", Step: step, Data: data}
}

func TestPlainProgressIsOneLinePerChange(t *testing.T) {
	p, out, clk := newTestProgress(modePlain)
	p.Header("Deploying quake · Dockerfile")

	p.Begin("upload")
	clk.advance(600 * time.Millisecond)
	p.Done("upload", "1.4 MB")
	send(t, p, stepEv("build", "start", ""), outputEv("build", "#1 load\n"))
	clk.advance(9100 * time.Millisecond)
	send(t, p, stepEv("build", "done", "image v7"), stepEv("release", "start", ""),
		stepEv("release", "progress", "waiting for health check"))
	clk.advance(18400 * time.Millisecond)
	send(t, p, stepEv("release", "done", "1/1 units healthy"), stepEv("route", "start", ""))
	clk.advance(300 * time.Millisecond)
	p.Finish(nil)
	p.Summary("https://quake.example")

	want := `Deploying quake · Dockerfile
→ Upload
✓ Upload 1.4 MB (0.6s)
→ Build
✓ Build image v7 (9.1s)
→ Release
✓ Release 1/1 units healthy (18s)
→ Route
✓ Route (0.3s)
https://quake.example
`
	if out.String() != want {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestPlainProgressPrintsTheFailingStepInFull(t *testing.T) {
	p, out, _ := newTestProgress(modePlain)
	send(t, p,
		stepEv("build", "start", ""),
		outputEv("build", "#5 RUN go build ./...\n#5 0.812 ./main.go:12:2: undefined: foo\n"),
	)
	p.Finish(errors.New("deploy failed"))

	got := out.String()
	if !strings.Contains(got, "✗ Build (0.0s)\n\n    #5 RUN go build ./...\n    #5 0.812 ./main.go:12:2: undefined: foo\n\n") {
		t.Errorf("output:\n%s", got)
	}
	if strings.Contains(got, "Release") {
		t.Errorf("no step after the failing one should appear:\n%s", got)
	}
}

func TestProgressWithAnOlderAPIShowsWhatItSaid(t *testing.T) {
	for _, err := range []error{nil, errors.New("deploy failed")} {
		p, out, _ := newTestProgress(modePlain)
		p.Begin("upload")
		p.Done("upload", "1 KB")
		send(t, p, outputEv("", "---> building\n"))
		p.Finish(err)
		if !strings.Contains(out.String(), "---> building\n") {
			t.Errorf("err=%v: the untagged output was lost:\n%s", err, out.String())
		}
	}
}

func TestVerboseProgressPassesEveryLineThrough(t *testing.T) {
	p, out, _ := newTestProgress(modeVerbose)
	send(t, p, stepEv("build", "start", ""), outputEv("build", "#1 load\n"))
	if out.String() != "#1 load\n" {
		t.Errorf("output = %q, want the raw line and no step rows", out.String())
	}
	p.Finish(errors.New("x"))
	if strings.Contains(out.String(), "✗") {
		t.Errorf("verbose should leave failure reporting to the error: %q", out.String())
	}
}

func TestProgressKeepsOnlyTheLastLinesOfAStep(t *testing.T) {
	p, _, _ := newTestProgress(modePlain)
	send(t, p, stepEv("build", "start", ""))
	for i := 0; i < maxStepLines+10; i++ {
		send(t, p, outputEv("build", "line\n"))
	}
	if n := len(p.steps[0].lines); n != maxStepLines {
		t.Errorf("kept %d lines, want %d", n, maxStepLines)
	}
}

func TestProgressAttributesAPartialLineToItsOwnStep(t *testing.T) {
	p, _, _ := newTestProgress(modePlain)
	send(t, p,
		stepEv("build", "start", ""),
		outputEv("build", "foo"),
		stepEv("build", "done", ""),
		stepEv("release", "start", ""),
		outputEv("release", "bar\n"),
	)
	if got := p.steps[0].lines; len(got) != 1 || got[0] != "foo" {
		t.Errorf("build lines = %v, want [\"foo\"]", got)
	}
	if got := p.steps[1].lines; len(got) != 1 || got[0] != "bar" {
		t.Errorf("release lines = %v, want [\"bar\"]", got)
	}
}

func TestProgressMarksFailureAfterTheLastStepWhenNoneIsRunning(t *testing.T) {
	p, out, _ := newTestProgress(modePlain)
	send(t, p, stepEv("build", "start", ""), outputEv("build", "#1 load\n"), stepEv("build", "done", "image v7"))
	p.Finish(errors.New("x"))

	got := out.String()
	if !strings.Contains(got, "✓ Build image v7") {
		t.Errorf("missing the success row for the finished step:\n%s", got)
	}
	if !strings.Contains(got, "✗ Failed after Build\n\n    #1 load\n") {
		t.Errorf("missing the failure marker for the last step:\n%s", got)
	}
}

func TestFmtDuration(t *testing.T) {
	cases := map[time.Duration]string{
		600 * time.Millisecond:   "0.6s",
		9100 * time.Millisecond:  "9.1s",
		18400 * time.Millisecond: "18s",
		65 * time.Second:         "1m05s",
	}
	for d, want := range cases {
		if got := fmtDuration(d); got != want {
			t.Errorf("fmtDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestLiveFrameShowsTheRunningStepAndItsTail(t *testing.T) {
	p, _, clk := newTestProgress(modeLive)
	send(t, p, stepEv("build", "start", ""))
	send(t, p, outputEv("build", "one\ntwo\n\nthree\nfour\n"))
	clk.advance(2 * time.Second)

	f := p.frame()
	if len(f) != 4 {
		t.Fatalf("frame = %q, want the row and a 3-line tail", f)
	}
	if !strings.HasPrefix(f[0], "  ⠋ Build") || !strings.HasSuffix(f[0], "2.0s") {
		t.Errorf("row = %q", f[0])
	}
	for i, want := range []string{"two", "three", "four"} {
		if f[i+1] != "      │ "+want {
			t.Errorf("tail[%d] = %q, want %q", i, f[i+1], want)
		}
	}
}

func TestLiveFrameTruncatesToTheTerminal(t *testing.T) {
	p, _, _ := newTestProgress(modeLive)
	send(t, p, stepEv("build", "start", ""), outputEv("build", strings.Repeat("x", 200)+"\n"))
	for _, l := range p.frame() {
		if n := len([]rune(l)); n > p.width-1 {
			t.Errorf("line of %d runes on a %d-column terminal: %q", n, p.width, l)
		}
	}
}

func TestLiveFrameNeverWrapsOnANarrowTerminal(t *testing.T) {
	p, _, _ := newTestProgress(modeLive)
	p.width = 30
	send(t, p, stepEv("build", "start", ""),
		stepEv("build", "progress", "waiting for health check on every unit"))

	for _, l := range p.frame() {
		if n := len([]rune(l)); n > p.width-1 {
			t.Errorf("line of %d runes on a %d-column terminal: %q", n, p.width, l)
		}
	}
}

func TestLiveProgressLeavesFinishedStepsAndNoLiveArea(t *testing.T) {
	p, out, _ := newTestProgress(modeLive)
	send(t, p, stepEv("build", "start", ""), outputEv("build", "#1 load\n"), stepEv("build", "done", "image v7"))
	p.Finish(nil)

	if f := p.frame(); len(f) != 0 {
		t.Errorf("live area still drawn: %q", f)
	}
	if !strings.Contains(out.String(), "✓ Build") || !strings.Contains(out.String(), "image v7") {
		t.Errorf("output = %q", out.String())
	}
}
