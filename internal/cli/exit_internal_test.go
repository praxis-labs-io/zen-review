package cli

import (
	"errors"
	"testing"
)

// The status and whether the error is printed are one decision, and a close
// failure travelling with the matched sentinel is a failure.
//
// This test is in the package because the sentinel is: the thing under test is
// ExitCode and Quiet, and there is no way to hand them a joined sentinel from
// outside. Every command returns it through a deferred close today, which is
// what keeps it alone, and nothing about that is enforced by the compiler.
func TestTheMatchedStatusDoesNotCoverAnErrorTravellingWithIt(t *testing.T) {
	closed := errors.New("closing the database: disk full")

	for _, tc := range []struct {
		name  string
		err   error
		code  int
		quiet bool
	}{
		{name: "nothing happened", err: nil, code: exitOK},
		{name: "the filter matched", err: errMatched, code: exitMatched, quiet: true},
		{name: "the filter matched, joined with nothing",
			err: errors.Join(errMatched, nil), code: exitMatched, quiet: true},
		{name: "a plain failure", err: closed, code: exitFailed},
		{name: "the filter matched and the close failed",
			err: errors.Join(errMatched, closed), code: exitFailed},
		{name: "the same the other way round",
			err: errors.Join(closed, errMatched), code: exitFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.err); got != tc.code {
				t.Errorf("ExitCode = %d, want %d", got, tc.code)
			}
			if got := Quiet(tc.err); got != tc.quiet {
				t.Errorf("Quiet = %t, want %t: a failure with something to say would be swallowed", got, tc.quiet)
			}
		})
	}
}
