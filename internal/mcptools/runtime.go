package mcptools

import (
	"bytes"
	"context"
	"time"

	"github.com/coffeece/goship/internal/portal"
)

type LogsArgs struct {
	AppArg
	Lines  int      `json:"lines,omitempty" jsonschema:"how many recent lines; default 100, at most 1000"`
	Source string   `json:"source,omitempty" jsonschema:"only lines from this source, e.g. app or router"`
	Units  []string `json:"units,omitempty" jsonschema:"only lines from these unit ids"`
}

type LogList struct {
	Entries []portal.LogEntry `json:"entries"`
}

type RunArgs struct {
	AppArg
	Command  string `json:"command" jsonschema:"shell command to run inside the app's image, e.g. python manage.py migrate"`
	Once     bool   `json:"once,omitempty" jsonschema:"run in one unit only (default: every unit)"`
	Isolated bool   `json:"isolated,omitempty" jsonschema:"run in a fresh unit instead of the running ones"`
}

type RunResult struct {
	Output string `json:"output"`
}

const (
	defaultLogLines = 100
	maxLogLines     = 1000
	runTimeout      = 10 * time.Minute
)

func (r *registry) runtime() {
	tool(r, "goship_logs", "Read an app's recent log lines. Never follows; call again for newer lines.", read,
		func(ctx context.Context, c *portal.Client, org string, in LogsArgs) (LogList, error) {
			lines := in.Lines
			switch {
			case lines <= 0:
				lines = defaultLogLines
			case lines > maxLogLines:
				lines = maxLogLines
			}
			out := LogList{Entries: []portal.LogEntry{}}
			err := c.Logs(ctx, org, in.App, portal.LogOptions{Lines: lines, Source: in.Source, Units: in.Units}, func(e portal.LogEntry) error {
				out.Entries = append(out.Entries, e)
				return nil
			})
			return out, err
		})
	tool(r, "goship_run", "Run a one-off command in the app's image and return its output. Capped at 10 minutes.", write,
		func(ctx context.Context, c *portal.Client, org string, in RunArgs) (RunResult, error) {
			ctx, cancel := context.WithTimeout(ctx, runTimeout)
			defer cancel()
			var buf bytes.Buffer
			err := c.Run(ctx, org, in.App, in.Command, in.Once, in.Isolated, &buf)
			return RunResult{Output: buf.String()}, err
		})
}
