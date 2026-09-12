package diff_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/golden"
)

// Inputs are real git output captured by testdata/fixtures.sh, discovered rather than listed.
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
