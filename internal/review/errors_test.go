package review_test

import (
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/review"
)

// fang title-cases the first word of an error, which would turn origin/main into Origin/Main.
func TestNoErrorMessageBeginsWithAValueTheCallerSupplied(t *testing.T) {
	const marker = "zzmarkerzz"

	for _, err := range []error{
		&review.TooLargeError{Count: 6000, Limit: 5000, Dir: marker, InDir: 5900},
		&review.TooLargeError{Count: 6000, Limit: 5000},
		&review.StaleGenerationError{Seq: 3, Current: 4},
		&review.StaleGenerationError{Seq: 3},
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
