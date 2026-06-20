// Package errs defines the stable exit codes used by repocache and a
// typed error that carries one. main.go converts these to process exit
// status.
package errs

import "fmt"

// Exit codes as documented in SPEC §9.
const (
	OK       = 0
	NotFound = 2
	Exists   = 3
	Dirty    = 4
	Locked   = 5
	Network  = 6
	Config   = 7
)

// Coded wraps an error with a specific exit code. main.go inspects this
// type to choose the process exit status.
type Coded struct {
	Code int
	Err  error
}

func (c *Coded) Error() string { return c.Err.Error() }
func (c *Coded) Unwrap() error { return c.Err }

// New constructs a Coded error from a code and a formatted message.
func New(code int, format string, a ...any) *Coded {
	return &Coded{Code: code, Err: fmt.Errorf(format, a...)}
}

// Wrap attaches a code to an existing error.
func Wrap(code int, err error) *Coded {
	return &Coded{Code: code, Err: err}
}
