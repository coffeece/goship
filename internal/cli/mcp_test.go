package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPServesToolsOverStdio drives `goship mcp` through pipes the way an
// agent would, and checks a tool call reaches the API with the login.
func TestMCPServesToolsOverStdio(t *testing.T) {
	var gotAuth, gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.Write([]byte(`[{"name":"api"}]`)) //nolint:errcheck
	})
	t.Setenv("GOSHIP_TOKEN", "tok")

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	root := NewRoot("test")
	root.SetArgs([]string{"mcp"})
	root.SetIn(inR)
	root.SetOut(outW)
	root.SetErr(&strings.Builder{})
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()

	client := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "goship_list_apps", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	if gotAuth != "Bearer tok" || gotPath != "/api/v1/orgs/acme/apps" {
		t.Errorf("API got %q %s", gotAuth, gotPath)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), `"api"`) {
		t.Errorf("structured content %s", raw)
	}
	cs.Close() //nolint:errcheck
	inW.Close()
	if err := <-done; err != nil {
		t.Errorf("server exited with %v", err)
	}
}

func TestMCPWithoutALoginExplainsItself(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected %s", r.URL) })

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	root := NewRoot("test")
	root.SetArgs([]string{"mcp"})
	root.SetIn(inR)
	root.SetOut(outW)
	root.SetErr(&strings.Builder{})
	go root.Execute() //nolint:errcheck

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), &mcp.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close() //nolint:errcheck
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "goship_whoami", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if !res.IsError || !strings.Contains(b.String(), "goship login") {
		t.Errorf("got isError=%v %q", res.IsError, b.String())
	}
	inW.Close()
}
