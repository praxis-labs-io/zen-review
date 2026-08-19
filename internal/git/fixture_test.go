package git

import (
	"os"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/testrepo"
)

func TestMain(m *testing.M) { os.Exit(testrepo.Main(m)) }

// fixture is a real repository plus the one thing testrepo cannot give it: a
// Repo of this package, which is unexported and cannot be built from outside.
type fixture struct {
	*testrepo.Repo
	t *testing.T
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{Repo: testrepo.New(t), t: t}
}

// open is the Repo under test, opened on the fixture.
func (f *fixture) open() *Repo {
	f.t.Helper()

	repo, err := Open(f.t.Context(), f.Dir())
	if err != nil {
		f.t.Fatalf("opening the fixture: %v", err)
	}
	return repo
}
