package cli

import "errors"

// The statuses this tool leaves with.
//
// A command that answered and a command that broke are different outcomes, and a
// hook reading only "non-zero" blocks an agent for whichever one happened: a git
// failure would read as an open comment. diff and grep already split them this
// way, so a reader knows the numbers without being told.
const (
	exitOK      = 0
	exitMatched = 1
	exitFailed  = 2
)

// errMatched is comments --exit-code reporting that the filter found something.
// It is not a failure and it says nothing: the command already wrote the
// comments it is reporting on.
//
// It is raised where nothing can be joined onto it. errors.Is finds it inside a
// join too, so a close failure travelling with it would come back here as a
// match and be swallowed by the handler that keeps a match quiet.
var errMatched = errors.New("the filter matched")

// ExitCode is the status the process leaves with, given whatever the root
// command returned.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errMatched):
		return exitMatched
	default:
		return exitFailed
	}
}

// Quiet reports an error raised to set an exit status rather than to say
// anything, which is the one a caller must not print.
func Quiet(err error) bool { return errors.Is(err, errMatched) }
