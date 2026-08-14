package store

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zen-review/zen-review/internal/diff"
)

// These tests are inside the package because what they check has no public
// surface. The pragmas and the schema version are what every promise above them
// rests on, and a test that could only reach them through the API would be
// asserting something else.

func openHere(t *testing.T) *DB {
	t.Helper()

	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "zen-review", "state.db"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing the database: %v", err)
		}
	})
	return db
}

func TestTheConnectionCarriesItsPragmas(t *testing.T) {
	db := openHere(t)

	// WAL is what lets two instances on one repository read while one writes, and
	// the busy timeout is what keeps the one that arrives second from failing
	// outright. foreign_keys is off by default in SQLite, so every cascade in the
	// schema is only real because of this line.
	for _, tc := range []struct{ pragma, want string }{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
	} {
		var got string
		if err := db.handle.QueryRowContext(t.Context(), "PRAGMA "+tc.pragma).Scan(&got); err != nil {
			t.Fatalf("reading %s: %v", tc.pragma, err)
		}
		if got != tc.want {
			t.Errorf("%s = %s, want %s", tc.pragma, got, tc.want)
		}
	}
}

func TestTheSchemaVersionMatchesTheLastMigration(t *testing.T) {
	db := openHere(t)

	files, err := embedded()
	if err != nil {
		t.Fatalf("reading the migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no migrations are embedded")
	}
	last := files[len(files)-1]

	got, err := schemaVersion(t.Context(), db.handle)
	if err != nil {
		t.Fatalf("reading the schema version: %v", err)
	}
	if got != last.version {
		t.Errorf("user_version = %d, want %d from %s", got, last.version, last.name)
	}
}

// The embedded files sort as text and apply in that order, so a number that is
// not zero-padded to the same width silently reorders the run. 10 lands before
// 2, the version recorded makes 2 look already applied, and it never runs. The
// check is against the parsed list rather than the real embed.FS, because there
// is one migration today and the failure has to be catchable before a second one
// is written badly.
func TestMigrationsWhoseNumbersDoNotClimbAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []migration
		ok    bool
	}{
		{
			name:  "padded to one width",
			files: []migration{{name: "0001_init.sql", version: 1}, {name: "0002_next.sql", version: 2}},
			ok:    true,
		},
		{
			name:  "unpadded, so ten sorts before two",
			files: []migration{{name: "1_init.sql", version: 1}, {name: "10_next.sql", version: 10}, {name: "2_later.sql", version: 2}},
		},
		{
			name:  "two files claiming one version",
			files: []migration{{name: "0001_init.sql", version: 1}, {name: "0001_again.sql", version: 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ordered(tc.files)
			if tc.ok {
				if err != nil {
					t.Fatalf("ordered = %v, want it accepted", err)
				}
				return
			}
			if err == nil {
				t.Fatal("the migrations should have been refused")
			}
			if !strings.Contains(err.Error(), "out of order") {
				t.Errorf("err = %v, want it to say what is wrong", err)
			}
		})
	}
}

// A database a newer build migrated is refused rather than half read. The
// worktree case is real: one checkout on a released binary, another on a build
// from source, both against the database the common dir holds.
func TestADatabaseFromANewerBuildIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zen-review", "state.db")

	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	files, err := embedded()
	if err != nil {
		t.Fatalf("reading the migrations: %v", err)
	}
	ahead := files[len(files)-1].version + 1
	if _, err := db.handle.ExecContext(t.Context(), fmt.Sprintf("PRAGMA user_version = %d", ahead)); err != nil {
		t.Fatalf("moving the schema version ahead: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing the database: %v", err)
	}

	reopened, err := Open(t.Context(), path)
	if err == nil {
		_ = reopened.Close()
		t.Fatal("a database from a newer build should not open")
	}
	if !strings.Contains(err.Error(), "upgrade zen-review") {
		t.Errorf("err = %v, want it to say what to do about it", err)
	}
}

// Running migrate twice has to be a no-op. Every command opens the database, so
// this is the path taken far more often than the first one.
func TestMigratingAnUpToDateDatabaseChangesNothing(t *testing.T) {
	db := openHere(t)

	var before int
	if err := db.handle.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&before); err != nil {
		t.Fatalf("reading the schema version: %v", err)
	}
	if err := db.migrate(t.Context()); err != nil {
		t.Fatalf("migrating an up-to-date database: %v", err)
	}

	var after int
	if err := db.handle.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&after); err != nil {
		t.Fatalf("reading the schema version: %v", err)
	}
	if after != before {
		t.Errorf("user_version moved from %d to %d", before, after)
	}
}

// The spec's five tables and no more. A sixth is a design decision, and it
// should not be able to arrive without this line changing.
func TestTheSchemaIsTheFiveTablesTheSpecNames(t *testing.T) {
	db := openHere(t)

	rows, err := db.handle.QueryContext(t.Context(),
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatalf("listing the tables: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("reading a table name: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("listing the tables: %v", err)
	}

	want := []string{"comments", "gen_files", "generations", "reviewed_ranges", "sessions"}
	if len(got) != len(want) {
		t.Fatalf("tables = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tables = %v, want %v", got, want)
		}
	}
}

// The vocabulary is closed, so a status outside it is a bug above this layer
// rather than a row to keep. The gitlink was the case that held this constraint
// back, and it needs no value of its own: git records an embedded repository as
// a mode 160000 entry that diffs as an ordinary added, modified or deleted file.
func TestGenFileStatusIsConstrainedToTheVocabulary(t *testing.T) {
	db := openHere(t)
	ctx := t.Context()

	now := time.Now().UTC().Truncate(time.Second)
	if err := db.SaveSession(ctx, Session{ID: "s", RepoPath: "/repo", Kind: KindBranch, Branch: "main", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("saving the session: %v", err)
	}

	add := func(status diff.Status) error {
		_, err := db.AddGeneration(ctx,
			Generation{SessionID: "s", BaseSha: "b", HeadSha: "h", CommitSha: "c", CreatedAt: now},
			[]GenFile{{Path: "a.txt", Status: status}}, Advance{})
		return err
	}

	for _, status := range []diff.Status{diff.FileAdded, diff.FileModified, diff.FileDeleted, diff.FileRenamed, diff.FileCopied} {
		if err := add(status); err != nil {
			t.Errorf("status %q was refused: %v", status, err)
		}
	}
	if err := add(diff.Status("submodule")); err == nil {
		t.Error("a status outside the vocabulary should be refused")
	}
}

// Go cannot put anything but 0 or 1 in this column, so the CHECK is what stops a
// hand-edited database from reaching a scan into a bool and failing there,
// nowhere near the write that did it.
func TestGenFileCutIsConstrainedToABoolean(t *testing.T) {
	db := openHere(t)
	ctx := t.Context()

	now := time.Now().UTC().Truncate(time.Second)
	if err := db.SaveSession(ctx, Session{ID: "s", RepoPath: "/repo", Kind: KindBranch, Branch: "main", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("saving the session: %v", err)
	}

	g, err := db.AddGeneration(ctx,
		Generation{SessionID: "s", BaseSha: "b", HeadSha: "h", CommitSha: "c", CreatedAt: now},
		[]GenFile{{Path: "a.txt", Status: diff.FileModified}}, Advance{})
	if err != nil {
		t.Fatalf("adding the generation: %v", err)
	}

	const set = "UPDATE gen_files SET cut = ? WHERE generation_id = ? AND path = 'a.txt'"
	for _, ok := range []int{0, 1} {
		if _, err := db.handle.ExecContext(ctx, set, ok, g.ID); err != nil {
			t.Errorf("cut = %d was refused: %v", ok, err)
		}
	}
	if _, err := db.handle.ExecContext(ctx, set, 2, g.ID); err == nil {
		t.Error("cut = 2 should be refused")
	}
}

// The comments table has no Go writer yet, so the rows below are raw SQL. What
// is under test is the schema, and a row type would only be a second way to
// spell it.

// anchored is a session with two generations, which is the least a comment
// needs: the one it was created at, and the one a refresh has since moved it
// onto.
func anchored(t *testing.T, db *DB) (created, current int64) {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Second)
	if err := db.SaveSession(t.Context(),
		Session{ID: "s", RepoPath: "/repo", Kind: KindBranch, Branch: "main", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("saving the session: %v", err)
	}

	var ids [2]int64
	for i := range ids {
		g, err := db.AddGeneration(t.Context(),
			Generation{SessionID: "s", BaseSha: "b", HeadSha: "h", CommitSha: "c", CreatedAt: now},
			[]GenFile{{Path: "a.txt", Status: diff.FileModified}}, Advance{})
		if err != nil {
			t.Fatalf("adding a generation: %v", err)
		}
		ids[i] = g.ID
	}
	return ids[0], ids[1]
}

// commentRow is the columns the tests below vary. Everything else is held
// constant by writeComment.
type commentRow struct {
	id      string
	gen     int64
	created int64
	scope   string
	state   string
	start   int
	end     int
}

func writeComment(t *testing.T, db *DB, c commentRow) error {
	t.Helper()

	const q = `
		INSERT INTO comments (
			id, session_id, generation_id, created_generation_id,
			path, side, start_line, end_line, scope, body, state,
			created_at, updated_at)
		VALUES (?, 's', ?, ?, 'a.txt', 'head', ?, ?, ?, 'this reads backwards', ?,
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`

	state := c.state
	if state == "" {
		state = "open"
	}
	_, err := db.handle.ExecContext(t.Context(), q, c.id, c.gen, c.created, c.start, c.end, c.scope, state)
	return err
}

// 'session' is gone: sessions.summary already holds the note about the whole
// review, and a comment scoped to nothing in particular was the second way to
// write it.
//
// The refused rows carry lines, so the CHECK on scope is the only thing that can
// turn them away. Written without them they would fail the agreement CHECK
// instead, and this would pass against a vocabulary that had never been
// narrowed.
func TestACommentIsScopedToALineARangeOrAFile(t *testing.T) {
	db := openHere(t)
	created, _ := anchored(t, db)

	for _, tc := range []struct {
		scope      string
		start, end int
		ok         bool
	}{
		{scope: "line", start: 4, end: 4, ok: true},
		{scope: "range", start: 4, end: 9, ok: true},
		{scope: "file", ok: true},
		{scope: "session", start: 4, end: 9},
		{scope: "hunk", start: 4, end: 9},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			err := writeComment(t, db, commentRow{
				id: tc.scope, gen: created, created: created,
				scope: tc.scope, start: tc.start, end: tc.end,
			})
			if tc.ok && err != nil {
				t.Fatalf("the scope %q was refused: %v", tc.scope, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("the scope %q should be refused", tc.scope)
			}
		})
	}
}

// A scope is a claim about what the comment is on, and the lines are how that
// claim is kept. The two disagreeing is a comment that cannot be translated as
// what it says it is.
func TestACommentsScopeAndItsLinesHaveToAgree(t *testing.T) {
	db := openHere(t)
	created, _ := anchored(t, db)

	for _, tc := range []struct {
		name       string
		scope      string
		start, end int
		ok         bool
	}{
		{name: "a range over lines", scope: "range", start: 4, end: 9, ok: true},
		{name: "a file over none", scope: "file", ok: true},

		// A selection of one is still a selection, so a range keeps its scope
		// where a line comment would have been written the same way.
		{name: "a range over one line", scope: "range", start: 4, end: 4, ok: true},

		{name: "a file carrying lines", scope: "file", start: 4, end: 9},
		{name: "a range carrying none", scope: "range"},
		{name: "a line carrying none", scope: "line"},
		{name: "a line over a span", scope: "line", start: 4, end: 9},
		{name: "a range ending before it starts", scope: "range", start: 9, end: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := writeComment(t, db, commentRow{
				id: tc.name, gen: created, created: created,
				scope: tc.scope, start: tc.start, end: tc.end,
			})
			if tc.ok && err != nil {
				t.Fatalf("%s was refused: %v", tc.name, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("%s should be refused", tc.name)
			}
		})
	}
}

// generation_id moves forward on every refresh that translates the anchor.
// created_generation_id is the column that can still say where the comment
// started, which is the tree anchor_blob resolves against.
func TestACommentRemembersTheGenerationItWasWrittenAt(t *testing.T) {
	db := openHere(t)
	ctx := t.Context()
	created, current := anchored(t, db)

	if err := writeComment(t, db, commentRow{
		id: "c", gen: created, created: created, scope: "line", start: 4, end: 4,
	}); err != nil {
		t.Fatalf("writing the comment: %v", err)
	}

	const move = "UPDATE comments SET generation_id = ?, start_line = 11, end_line = 11 WHERE id = 'c'"
	if _, err := db.handle.ExecContext(ctx, move, current); err != nil {
		t.Fatalf("moving the anchor onto the new generation: %v", err)
	}

	var at, from int64
	const read = "SELECT generation_id, created_generation_id FROM comments WHERE id = 'c'"
	if err := db.handle.QueryRowContext(ctx, read).Scan(&at, &from); err != nil {
		t.Fatalf("reading the comment back: %v", err)
	}
	if at != current {
		t.Errorf("generation_id = %d, want the generation the refresh moved it onto, %d", at, current)
	}
	if from != created {
		t.Errorf("created_generation_id = %d, want the one it was written at, %d", from, created)
	}

	// Both columns are real references, so a comment cannot claim a generation
	// that is not there.
	if err := writeComment(t, db, commentRow{
		id: "gone", gen: current, created: current + 1000, scope: "file",
	}); err == nil {
		t.Error("a comment created at a generation that does not exist should be refused")
	}
}

// Dropping a table drops its indexes. A rebuild that forgets to make them again
// costs nothing that fails, only every listing walking the table.
func TestTheCommentsIndexesSurviveTheRebuild(t *testing.T) {
	db := openHere(t)

	rows, err := db.handle.QueryContext(t.Context(),
		`SELECT name FROM sqlite_master
		 WHERE type = 'index' AND tbl_name = 'comments' AND name NOT LIKE 'sqlite_%'
		 ORDER BY name`)
	if err != nil {
		t.Fatalf("listing the indexes: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("reading an index name: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("listing the indexes: %v", err)
	}

	want := []string{"comments_by_generation", "comments_by_state"}
	if !slices.Equal(got, want) {
		t.Errorf("indexes = %v, want %v", got, want)
	}
}
