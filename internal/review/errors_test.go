package review_test

import (
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/review"
)

// Every one of these reaches a terminal through fang, which renders an error
// with a style carrying Transform(titleFirstWord): it title-cases
// strings.Fields(s)[0] before printing. A message opening with a ref hands the
// reader back Origin/Main, mangled inside the sentence telling them which ref to
// type, and a branch called feature/x becomes Feature/X.
//
// So the first word has to be a literal the message chose, never a value the
// caller supplied. Every field is filled with one marker, and the check is that
// none of it reached the front.
func TestNoErrorMessageBeginsWithAValueTheCallerSupplied(t *testing.T) {
	const marker = "zzmarkerzz"

	for _, err := range []error{
		&review.StackedError{Detected: marker, Candidates: []review.Candidate{{Branch: marker}}},
		&review.NoMergeBaseError{Ref: marker},
		&review.UnresolvableBaseError{Ref: marker},
		&review.TooLargeError{Count: 6000, Limit: 5000, Dir: marker, InDir: 5900},
		&review.TooLargeError{Count: 6000, Limit: 5000},
	} {
		message := err.Error()
		fields := strings.Fields(message)
		if len(fields) == 0 {
			t.Errorf("%T has an empty message", err)
			continue
		}
		if strings.Contains(fields[0], marker) {
			t.Errorf("%T opens with a value the caller passed, which fang will title-case: %q", err, message)
		}
	}
}
