package cli

import "errors"

// 1 is an answer and 2 a failure, split the way diff and grep split them.
const (
	exitOK      = 0
	exitMatched = 1
	exitFailed  = 2
)

// Raised only once the session is closed, so no close error is ever joined onto it.
var errMatched = errors.New("the filter matched")

// ExitCode returns the process status for the root command's error: 0, 1 for a --exit-code match, 2 otherwise.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return exitOK
	case matched(err):
		return exitMatched
	default:
		return exitFailed
	}
}

// Quiet reports whether err only sets the exit status and must not be printed.
func Quiet(err error) bool { return matched(err) }

// A plain errors.Is would read a close failure joined onto the sentinel as a match.
func matched(err error) bool {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return errors.Is(err, errMatched)
	}

	for _, e := range joined.Unwrap() {
		if !matched(e) {
			return false
		}
	}
	return true
}
