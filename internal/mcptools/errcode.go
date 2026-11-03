package mcptools

import "github.com/coffeece/goship/internal/portal"

// codedError is a tool failure the API named: its text ends with a
// "code: <code>" line an agent can branch on without reading the message.
type codedError struct {
	err  error
	code string
}

func (e *codedError) Error() string { return e.err.Error() + "\n\ncode: " + e.code }
func (e *codedError) Unwrap() error { return e.err }

// withCode appends the API's code to err's text as its last line, keeping
// err in the chain. A failure the API did not name is returned as is.
//
// The code goes in the text only: the go-sdk replaces any result a handler
// returns alongside an error, so a failed call cannot carry _meta.
func withCode(err error) error {
	if err == nil {
		return nil
	}
	code := portal.ErrorCode(err)
	if code == "" {
		return err
	}
	return &codedError{err: err, code: code}
}
