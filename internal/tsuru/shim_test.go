package tsuru

import (
	"io"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tsurucmd "github.com/tsuru/tsuru-client/tsuru/cmd"
)

type cancelableCommand struct {
	started  chan struct{}
	release  chan struct{}
	mu       sync.Mutex
	canceled bool
}

func (c *cancelableCommand) Info() *tsurucmd.Info { return &tsurucmd.Info{Name: "fake"} }

func (c *cancelableCommand) Run(*tsurucmd.Context) error {
	close(c.started)
	<-c.release
	return nil
}

func (c *cancelableCommand) Cancel(tsurucmd.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.canceled = true
	close(c.release)
	return nil
}

func (c *cancelableCommand) wasCanceled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.canceled
}

// Ctrl-C has to reach the platform. Killing only the client leaves the deploy
// running and the app's event lock held, which is what produced
// "event locked: app(...) running app.deploy" with no way out.
func TestRunForwardsInterruptToTheCommand(t *testing.T) {
	cmd := &cancelableCommand{started: make(chan struct{}), release: make(chan struct{})}

	done := make(chan error, 1)
	go func() {
		done <- Run(cmd, nil, strings.NewReader(""), io.Discard, io.Discard)
	}()

	<-cmd.started
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interrupt never reached the command")
	}
	if !cmd.wasCanceled() {
		t.Error("Cancel was not called")
	}
}
