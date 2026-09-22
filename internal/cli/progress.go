package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coffeece/goship/internal/portal"
	"golang.org/x/term"
)

// stepTitles are the steps a deploy or rollback shows, in the reader's words.
// build, release and route come from the API; the others are the CLI's own.
var stepTitles = map[string]string{
	"create":      "Create",
	"environment": "Environment",
	"upload":      "Upload",
	"build":       "Build",
	"release":     "Release",
	"route":       "Route",
}

const (
	// maxStepLines is how much of a step's output is kept to print should it
	// fail.
	maxStepLines = 2000
	tailLines    = 3
)

type stepState int

const (
	stepRunning stepState = iota
	stepDone
	stepFailed
)

type step struct {
	key, detail string
	state       stepState
	start, end  time.Time
	lines       []string
}

func (s *step) title() string {
	if t, ok := stepTitles[s.key]; ok {
		return t
	}
	return s.key
}

func (s *step) add(line string) {
	s.lines = append(s.lines, line)
	if len(s.lines) > maxStepLines {
		s.lines = s.lines[len(s.lines)-maxStepLines:]
	}
}

type progressMode int

const (
	modePlain progressMode = iota
	modeLive
	modeVerbose
)

// progress shows a deploy or rollback as a short list of steps: redrawn in
// place on a terminal, one line per change anywhere else, and every line of
// output with --verbose.
type progress struct {
	out   io.Writer
	mode  progressMode
	color bool
	width int
	tick  time.Duration // spinner interval; 0 means no spinner
	now   func() time.Time

	mu       sync.Mutex
	header   string
	notes    []string
	started  bool // header printed
	finished bool
	began    time.Time
	steps    []*step
	partial  string
	pending  []string // output before any step: an API that reports none
	sawStep  bool
	version  string
	drawn    int // lines of the live area on screen
	spin     int
	stop     chan struct{}
}

func newProgress(out io.Writer, verbose bool) *progress {
	p := &progress{out: out, now: time.Now, width: 80}
	switch {
	case verbose:
		p.mode = modeVerbose
	case isTerminal(out):
		p.mode = modeLive
		p.color = os.Getenv("NO_COLOR") == ""
		p.tick = 100 * time.Millisecond
		if w, _, err := term.GetSize(int(out.(*os.File).Fd())); err == nil && w > 0 {
			p.width = w
		}
	}
	p.began = p.now()
	return p
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) && os.Getenv("TERM") != "dumb"
}

// Header sets the line shown above the steps, printed with the first of them.
func (p *progress) Header(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.header = fmt.Sprintf(format, args...)
}

// Note adds a line under the header.
func (p *progress) Note(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notes = append(p.notes, fmt.Sprintf(format, args...))
}

// Begin starts one of the CLI's own steps.
func (p *progress) Begin(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.begin(key)
}

// Done ends a running step, with what it produced (a size, a count).
func (p *progress) Done(key, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s := p.find(key); s != nil && s.state == stepRunning {
		p.finish(s, stepDone, detail)
	}
}

// Event takes one event of the API's stream.
func (p *progress) Event(ev portal.ReleaseEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch ev.Type {
	case "output":
		if p.mode == modeVerbose {
			_, err := io.WriteString(p.out, ev.Data)
			return err
		}
		p.output(ev.Step, ev.Data)
	case "step":
		if p.mode == modeVerbose {
			return nil
		}
		p.sawStep = true
		switch ev.State {
		case "start":
			p.begin(ev.Step)
		case "progress":
			if s := p.find(ev.Step); s != nil && s.state == stepRunning {
				s.detail = ev.Detail
				p.redraw()
			}
		case "done":
			if v, ok := strings.CutPrefix(ev.Detail, "image "); ok && ev.Step == "build" {
				p.version = v
			}
			if s := p.find(ev.Step); s != nil && s.state == stepRunning {
				p.finish(s, stepDone, ev.Detail)
			}
		}
	}
	return nil
}

// Finish closes the view. On success every step still open is done — a
// rollback's stream does not say the route ended. On failure the running step
// is marked failed and its whole output printed, since that is where the
// reason is. Calling it again does nothing.
func (p *progress) Finish(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finished {
		return
	}
	p.finished = true
	p.stopSpinner()
	if p.partial != "" {
		p.output("", "\n")
	}
	if !p.started && len(p.pending) == 0 {
		return
	}
	p.printHeader()
	cur := p.active()
	if err == nil {
		for _, s := range p.steps {
			if s.state == stepRunning {
				p.finish(s, stepDone, s.detail)
			}
		}
		if !p.sawStep && p.mode != modeVerbose {
			p.dump(p.pending, "")
		}
		return
	}
	if cur != nil {
		p.finish(cur, stepFailed, "")
	}
	if p.mode == modeVerbose {
		return
	}
	switch {
	case !p.sawStep:
		p.dump(p.pending, "")
	case cur != nil:
		p.dump(cur.lines, "    ")
	case len(p.steps) > 0:
		p.dump(p.steps[len(p.steps)-1].lines, "    ")
	}
}

// Summary ends a successful release on the address it is served at.
func (p *progress) Summary(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch p.mode {
	case modeLive:
		meta := fmtDuration(p.now().Sub(p.began))
		if p.version != "" {
			meta = p.version + " · " + meta
		}
		fmt.Fprintf(p.out, "\n  → %s   %s\n", url, p.paint("2", meta))
	case modeVerbose:
		fmt.Fprintf(p.out, "\n✓ %s\n", url)
	default:
		fmt.Fprintln(p.out, url)
	}
}

func (p *progress) begin(key string) {
	p.printHeader()
	if cur := p.active(); cur != nil {
		p.finish(cur, stepDone, cur.detail)
	}
	s := &step{key: key, start: p.now()}
	p.steps = append(p.steps, s)
	if p.mode == modeLive {
		p.startSpinner()
		p.redraw()
		return
	}
	fmt.Fprintf(p.out, "→ %s\n", s.title())
}

func (p *progress) finish(s *step, state stepState, detail string) {
	s.state, s.end = state, p.now()
	if detail != "" {
		s.detail = detail
	}
	if p.mode == modeLive {
		p.clearLive()
		fmt.Fprintln(p.out, p.liveRow(s))
		p.redraw()
		return
	}
	fmt.Fprintln(p.out, plainRow(s))
}

// output files the lines of data under the step the API tagged them with.
func (p *progress) output(stepKey, data string) {
	lines := strings.Split(p.partial+data, "\n")
	p.partial = lines[len(lines)-1]
	for _, l := range lines[:len(lines)-1] {
		l = strings.TrimRight(l, "\r")
		switch s := p.find(stepKey); {
		case stepKey != "" && s != nil:
			s.add(l)
		case len(p.steps) > 0 && p.sawStep:
			p.steps[len(p.steps)-1].add(l)
		default:
			p.pending = append(p.pending, l)
		}
	}
	p.redraw()
}

func (p *progress) printHeader() {
	if p.started {
		return
	}
	p.started = true
	if p.header != "" {
		fmt.Fprintln(p.out, p.header)
	}
	for _, n := range p.notes {
		if p.mode == modeLive {
			fmt.Fprintln(p.out, p.paint("2", n))
		} else {
			fmt.Fprintln(p.out, n)
		}
	}
	if p.mode == modeLive {
		fmt.Fprintln(p.out)
	}
}

// dump prints lines between blank lines, without the ones at either end.
func (p *progress) dump(lines []string, indent string) {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return
	}
	if indent != "" {
		fmt.Fprintln(p.out)
	}
	for _, l := range lines {
		fmt.Fprintln(p.out, indent+l)
	}
	if indent != "" {
		fmt.Fprintln(p.out)
	}
}

func (p *progress) find(key string) *step {
	for i := len(p.steps) - 1; i >= 0; i-- {
		if p.steps[i].key == key {
			return p.steps[i]
		}
	}
	return nil
}

func (p *progress) active() *step {
	if n := len(p.steps); n > 0 && p.steps[n-1].state == stepRunning {
		return p.steps[n-1]
	}
	return nil
}

func plainRow(s *step) string {
	mark := "✓"
	if s.state == stepFailed {
		mark = "✗"
	}
	row := mark + " " + s.title()
	if s.detail != "" {
		row += " " + s.detail
	}
	return row + " (" + fmtDuration(s.end.Sub(s.start)) + ")"
}

func fmtDuration(d time.Duration) string {
	switch {
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	default:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

func (p *progress) paint(code, s string) string {
	if !p.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// The live view is added in the next task; on a non-terminal these are no-ops.
func (p *progress) redraw()                {}
func (p *progress) clearLive()             {}
func (p *progress) startSpinner()          {}
func (p *progress) stopSpinner()           {}
func (p *progress) liveRow(s *step) string { return plainRow(s) }
