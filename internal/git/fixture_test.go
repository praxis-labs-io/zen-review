package git

import (
	"os"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/testrepo"
)

func TestMain(m *testing.M) { os.Exit(testrepo.Main(m)) }

type fixture struct {
	*testrepo.Repo
	t *testing.T
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{Repo: testrepo.New(t), t: t}
}

func (f *fixture) open() *Repo {
	f.t.Helper()

	repo, err := Open(f.t.Context(), f.Dir())
	if err != nil {
		f.t.Fatalf("opening the fixture: %v", err)
	}
	return repo
}
