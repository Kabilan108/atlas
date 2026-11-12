package cli

import (
	"errors"

	"github.com/kabilan108/atlas/internal/ops"
)

// Process exit codes, one per error kind, so scripts can tell a missing
// resource from rejected credentials without parsing messages.
const (
	ExitOK = 0
	// ExitError is any failure not classified below, including Bitbucket
	// and Jira server errors.
	ExitError = 1
	// ExitUsage is a command line or input atlas or the API rejected:
	// unknown commands and flags, wrong arguments, invalid values.
	ExitUsage = 2
	// ExitAuth is missing or rejected credentials, or a token without the
	// scope or account access a request needs.
	ExitAuth = 3
	// ExitNotFound is a missing repository, pull request, issue, branch,
	// or other resource.
	ExitNotFound = 4
	// ExitConflict is a write the API refused because of the resource's
	// state, such as a merge conflict.
	ExitConflict = 5
	// ExitRateLimited is an API rate limit.
	ExitRateLimited = 6
	// ExitPartial is output that was printed but is incomplete: some
	// entries of a batch failed, and the output says which.
	ExitPartial = 7
	// ExitPending is a deadline: a request timed out, or a merge, pipeline
	// run, or set of checks was still running when atlas stopped waiting.
	ExitPending = 8
)

// exitCodesHelp documents the codes in `atlas --help`.
const exitCodesHelp = `Exit codes:
  0  success
  1  other error, including Bitbucket or Jira server errors and failed checks
  2  usage: unknown command or flag, bad arguments, or input the API rejected
  3  authentication: missing or rejected credentials, or a missing scope
  4  not found
  5  conflict, such as a merge conflict
  6  rate limited
  7  partial: output was printed, but some entries failed
  8  timeout or still pending: a merge, pipeline run, or checks still running`

// ExitCode maps an error from Execute to the process exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	// Cobra rejects unknown commands, bad flags, wrong argument counts, and
	// missing required flags before any command runs.
	var ran *ranError
	if !errors.As(err, &ran) {
		return ExitUsage
	}
	var pending *pendingError
	if errors.As(err, &pending) {
		return ExitPending
	}
	switch ops.KindOf(err) {
	case ops.Invalid:
		return ExitUsage
	case ops.Auth:
		return ExitAuth
	case ops.NotFound:
		return ExitNotFound
	case ops.Conflict:
		return ExitConflict
	case ops.RateLimited:
		return ExitRateLimited
	case ops.Partial:
		return ExitPartial
	case ops.Timeout:
		return ExitPending
	}
	var usage *usageError
	if errors.As(err, &usage) {
		return ExitUsage
	}
	return ExitError
}

// ranError marks an error returned by a command's RunE, as opposed to one
// cobra raised while parsing the command line.
type ranError struct{ err error }

func (e *ranError) Error() string { return e.err.Error() }
func (e *ranError) Unwrap() error { return e.err }

// usageError is an error building an op's input from the command's
// arguments and flags, such as two flags that exclude each other.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// pendingError is work atlas started or watched that had not finished when
// it stopped waiting.
type pendingError struct{ err error }

func (e *pendingError) Error() string { return e.err.Error() }
func (e *pendingError) Unwrap() error { return e.err }

func pending(err error) error {
	return &pendingError{err: err}
}
