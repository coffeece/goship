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
