package portal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func streamServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL, "gsp_token", WithVersion("1.2.3"))
}

func TestDeployUploadsTheArchiveLastAndCopiesTheOutput(t *testing.T) {
	var parts []string
	var archive, version string
	c := streamServer(t, func(w http.ResponseWriter, r *http.Request) {
		version = r.Header.Get("X-Goship-Version")
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("MultipartReader: %v", err)
			return
		}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			parts = append(parts, p.FormName())
			if p.FormName() == "file" {
				archive = string(b)
			}
		}
		fmt.Fprintln(w, `{"type":"output","data":"---> building\n"}`)
		fmt.Fprintln(w, `{"type":"ping"}`)
		fmt.Fprintln(w, `{"type":"result","ok":true}`)
	})

	var out bytes.Buffer
	err := c.Deploy(context.Background(), "acme", "blog", DeployRequest{Archive: strings.NewReader("tarball"), Message: "fix", Dockerfile: "FROM scratch"}, printOutput(&out))
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if strings.Join(parts, ",") != "message,dockerfile,file" {
		t.Errorf("parts = %v, want the file last", parts)
	}
	if archive != "tarball" || out.String() != "---> building\n" || version != "1.2.3" {
		t.Errorf("archive=%q out=%q version=%q", archive, out.String(), version)
	}
}

func TestStreamReportsAFailedOperation(t *testing.T) {
	c := streamServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"output","data":"main.go:3: undefined: x\n"}`)
		fmt.Fprintln(w, `{"type":"result","ok":false,"error":"deploy failed: exit status 1"}`)
	})

	var out bytes.Buffer
	err := c.Rollback(context.Background(), "acme", "blog", "v3", printOutput(&out))
	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.Message != "deploy failed: exit status 1" {
		t.Fatalf("error = %v, want the API's account of the failure", err)
	}
	if !strings.Contains(out.String(), "undefined: x") {
		t.Errorf("output lost: %q", out.String())
	}
}

// A 200 says nothing about the outcome; only the result line does.
func TestStreamWithoutAResultIsAFailure(t *testing.T) {
	c := streamServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"output","data":"---> building\n"}`)
	})

	err := c.Run(context.Background(), "acme", "blog", "true", true, false, io.Discard)
	if !errors.Is(err, ErrStreamCut) {
		t.Errorf("error = %v, want ErrStreamCut", err)
	}
}

func TestStreamRefusedUpFrontIsAnAPIError(t *testing.T) {
	c := streamServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintln(w, `{"error":"data conflict: app \"blog\" is paused — wake it up before deploying"}`)
	})

	err := c.Deploy(context.Background(), "acme", "blog", DeployRequest{Archive: strings.NewReader("x")}, printOutput(io.Discard))
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || !strings.Contains(apiErr.Message, "paused") {
		t.Errorf("error = %v", err)
	}
}

func TestLogsPassesTheOptionsAndEveryLine(t *testing.T) {
	var query string
	c := streamServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		fmt.Fprintln(w, `{"type":"log","date":"2026-09-19T10:00:00Z","source":"web","unit":"u1","message":"one"}`)
		fmt.Fprintln(w, `{"type":"log","message":"two"}`)
		fmt.Fprintln(w, `{"type":"result","ok":true}`)
	})

	var got []string
	err := c.Logs(context.Background(), "acme", "blog", LogOptions{Lines: 5, Follow: true, Source: "web", Units: []string{"u1"}}, func(l LogEntry) error {
		got = append(got, l.Source+":"+l.Message)
		return nil
	})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if strings.Join(got, ",") != "web:one,:two" {
		t.Errorf("entries = %v", got)
	}
	for _, want := range []string{"lines=5", "follow=true", "source=web", "unit=u1"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q missing %q", query, want)
		}
	}
}

func TestReleaseEventsCarryTheirStep(t *testing.T) {
	c := streamServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"step","step":"build","state":"start"}`)
		fmt.Fprintln(w, `{"type":"output","step":"build","data":"#1 load\n"}`)
		fmt.Fprintln(w, `{"type":"ping"}`)
		fmt.Fprintln(w, `{"type":"step","step":"build","state":"done","detail":"image v7"}`)
		fmt.Fprintln(w, `{"type":"result","ok":true}`)
	})

	var got []ReleaseEvent
	err := c.Rollback(context.Background(), "acme", "blog", "v7", func(ev ReleaseEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	want := []ReleaseEvent{
		{Type: "step", Step: "build", State: "start"},
		{Type: "output", Step: "build", Data: "#1 load\n"},
		{Type: "step", Step: "build", State: "done", Detail: "image v7"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// printOutput handles a release by writing its output to w and ignoring its
// steps.
func printOutput(w io.Writer) func(ReleaseEvent) error {
	return func(ev ReleaseEvent) error {
		if ev.Type != "output" {
			return nil
		}
		_, err := io.WriteString(w, ev.Data)
		return err
	}
}
