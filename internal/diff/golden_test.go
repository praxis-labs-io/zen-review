package diff_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/golden"
)

// Every input under testdata is real git output, captured by
// testdata/fixtures.sh with the flags internal/git pins. Hand-written diff text
// would only test the parser against one idea of the format.
//
// Add a case to that script, rerun it, then `make golden`. The inputs are
// discovered here, so there is no list to keep in step.
func TestGoldenParses(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join("testdata", "*.diff"))
	if err != nil {
		t.Fatalf("looking for testdata: %v", err)
	}
	if len(inputs) == 0 {
		t.Fatal("no .diff inputs in testdata")
	}

	for _, input := range inputs {
		name := strings.TrimSuffix(filepath.Base(input), ".diff")

		t.Run(name, func(t *testing.T) {
			patch, err := os.ReadFile(input)
			if err != nil {
				t.Fatalf("reading %s: %v", input, err)
			}

			got, err := json.MarshalIndent(diff.Parse(patch), "", "  ")
			if err != nil {
				t.Fatalf("encoding the parse of %s: %v", name, err)
			}
			golden.Compare(t, name, append(got, '\n'))
		})
	}
}
