#!/usr/bin/env bash
#
# Captures the .diff inputs beside this script from real git output, using the
# same flags internal/git pins in production. Hand-written diff text would only
# test the parser against one idea of the format.
#
#   ./internal/diff/testdata/fixtures.sh internal/diff/testdata
#   make golden
#
# Each case builds its own repository, so a change to one leaves the others
# byte-identical and the golden diff stays readable.
set -euo pipefail

OUT="$1"
mkdir -p "$OUT"

export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_DATE=2026-01-01T00:00:00Z
export GIT_COMMITTER_DATE=2026-01-01T00:00:00Z

FLAGS=(--no-color --no-ext-diff --no-textconv --find-renames --full-index
       --unified=3 --src-prefix=a/ --dst-prefix=b/)

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# fresh starts a repo for one case and echoes its path.
fresh() {
  local d="$WORK/$1"
  mkdir -p "$d"
  git -C "$d" init -q -b main
  git -C "$d" config user.name Test
  git -C "$d" config user.email test@example.com
  echo "$d"
}

commit() { git -C "$1" add -A; git -C "$1" commit -q -m "${2:-base}"; }
# commitIndex commits what is already staged, leaving the index alone: a gitlink
# has no working-tree directory and `add -A` would take it straight back out.
commitIndex() { git -C "$1" commit -q -m "${2:-base}"; }

# capture writes the diff for a case, with any extra flags appended.
capture() {
  local d="$1" name="$2"; shift 2
  git -C "$d" -c core.quotePath=false diff "${FLAGS[@]}" "$@" --end-of-options HEAD -- > "$OUT/$name.diff" || true
  echo "  $name.diff  $(wc -l < "$OUT/$name.diff" | tr -d ' ') lines"
}

lines() { # lines <count> <marker-line> <marker-at>
  local n="$1" marker="$2" at="$3" i
  for ((i = 1; i <= n; i++)); do
    if [[ $i -eq $at ]]; then echo "$marker"; else echo "line $i"; fi
  done
}

d=$(fresh modify);            lines 20 "line 10" 10 > "$d/a.txt"; commit "$d"
lines 20 "changed" 10 > "$d/a.txt";                               capture "$d" modify

d=$(fresh add);               echo one > "$d/a.txt"; commit "$d"
printf 'first\nsecond\n' > "$d/new.txt"; git -C "$d" add -A;      capture "$d" add

d=$(fresh delete);            echo one > "$d/a.txt"; printf 'x\ny\nz\n' > "$d/gone.txt"; commit "$d"
rm "$d/gone.txt";                                                 capture "$d" delete

d=$(fresh rename);            lines 20 "line 10" 10 > "$d/old.txt"; commit "$d"
git -C "$d" mv old.txt new.txt;                                   capture "$d" rename

d=$(fresh rename_with_edits); lines 20 "line 10" 10 > "$d/old.txt"; commit "$d"
git -C "$d" mv old.txt new.txt; lines 20 "changed" 10 > "$d/new.txt"
                                                                  capture "$d" rename_with_edits

d=$(fresh copy);              lines 20 "line 10" 10 > "$d/a.txt"; commit "$d"
cp "$d/a.txt" "$d/b.txt"; git -C "$d" add -A;                     capture "$d" copy -C --find-copies-harder

d=$(fresh mode_only);         echo '#!/bin/sh' > "$d/run.sh"; commit "$d"
chmod +x "$d/run.sh";                                             capture "$d" mode_only

d=$(fresh binary);            echo one > "$d/a.txt"; commit "$d"
printf '\x89PNG\r\n\x1a\n\x00\x01\x02' > "$d/logo.png"; git -C "$d" add -A
                                                                  capture "$d" binary

d=$(fresh binary_with_space); echo one > "$d/a.txt"; commit "$d"
printf '\x00\x01binary\x00' > "$d/my file.bin"; git -C "$d" add -A
                                                                  capture "$d" binary_with_space

d=$(fresh submodule);         echo one > "$d/a.txt"; commit "$d"
sha=$(git -C "$d" rev-parse HEAD)
git -C "$d" update-index --add --cacheinfo "160000,$sha,sub"; commitIndex "$d" "add the gitlink"
other=$(git -C "$d" commit-tree "$(git -C "$d" rev-parse HEAD^{tree})" -p "$sha" -m moved)
git -C "$d" update-index --cacheinfo "160000,$other,sub"
git -C "$d" -c core.quotePath=false diff "${FLAGS[@]}" --cached --end-of-options HEAD -- > "$OUT/submodule.diff" || true
echo "  submodule.diff  $(wc -l < "$OUT/submodule.diff" | tr -d ' ') lines"

d=$(fresh no_newline_at_eof); printf 'one\ntwo\n' > "$d/a.txt"; commit "$d"
printf 'one\ntwo' > "$d/a.txt";                                   capture "$d" no_newline_at_eof

d=$(fresh multiple_hunks);    lines 40 "line 5" 5 > "$d/a.txt"; commit "$d"
lines 40 "line 5" 5 > "$d/a.txt"
awk 'NR==5 {print "changed near the top"; next} NR==35 {print "changed near the bottom"; next} {print}' \
  "$d/a.txt" > "$d/a.new" && mv "$d/a.new" "$d/a.txt";            capture "$d" multiple_hunks

d=$(fresh section_heading)
cat > "$d/main.go" <<'GO'
package main

func first() int {
	total := 0
	total += 1
	total += 2
	total += 3
	total += 4
	return total
}

func second() string {
	return "unchanged"
}
GO
commit "$d"
sed -i '' 's/total += 3/total += 30/' "$d/main.go";                capture "$d" section_heading

d=$(fresh one_line_ranges);   echo before > "$d/a.txt"; commit "$d"
echo after > "$d/a.txt";                                          capture "$d" one_line_ranges

d=$(fresh quoted_path);       echo one > "$d/a.txt"; commit "$d"
printf 'quoted\n' > "$d/say\"hi\".txt"; git -C "$d" add -A;       capture "$d" quoted_path

d=$(fresh crlf);              printf 'one\r\ntwo\r\nthree\r\n' > "$d/a.txt"; commit "$d"
printf 'one\r\nTWO\r\nthree\r\n' > "$d/a.txt";                    capture "$d" crlf

d=$(fresh conflicted_cc);     printf 'one\ntwo\nthree\n' > "$d/a.txt"; commit "$d"
git -C "$d" checkout -q -b side
printf 'one\nSIDE\nthree\n' > "$d/a.txt"; commit "$d" side
git -C "$d" checkout -q main
printf 'one\nMAIN\nthree\n' > "$d/a.txt"; commit "$d" main
git -C "$d" merge -q side 2>/dev/null || true
git -C "$d" -c core.quotePath=false diff "${FLAGS[@]}" > "$OUT/conflicted_cc.diff" || true
echo "  conflicted_cc.diff  $(wc -l < "$OUT/conflicted_cc.diff" | tr -d ' ') lines"

d=$(fresh empty_file);        echo one > "$d/a.txt"; commit "$d"
: > "$d/blank.txt"; git -C "$d" add -A;                           capture "$d" empty_file

# The untracked path production takes: --no-index against /dev/null, which must
# assemble into the same shape as a staged add.
d=$(fresh untracked);         echo one > "$d/a.txt"; commit "$d"
printf 'fresh\nfile\n' > "$d/new.txt"
git -C "$d" -c core.quotePath=false diff "${FLAGS[@]}" --no-index --end-of-options /dev/null new.txt > "$OUT/untracked.diff" || true
echo "  untracked.diff  $(wc -l < "$OUT/untracked.diff" | tr -d ' ') lines"

# A file whose own content is diff text. The removed line renders as "--- a sql
# comment", which is exactly the shape of a path header, and this repo's testdata
# guarantees the case is real rather than theoretical.
d=$(fresh diff_text)
cat > "$d/notes.md" <<'TXT'
diff --git a/one b/one
--- a/one
+++ b/one
@@ -1 +1 @@
-- a sql comment
++ not a header
TXT
commit "$d"
sed -i '' 's/-- a sql comment/-- replaced/' "$d/notes.md";        capture "$d" diff_text

d=$(fresh empty_file_removed); echo one > "$d/a.txt"; : > "$d/blank.txt"; commit "$d"
rm "$d/blank.txt";                                                capture "$d" empty_file_removed

# A binary file whose name needs quoting is where the "diff --git" line is the
# only place a path appears and it arrives quoted on both sides.
d=$(fresh binary_quoted_path); echo one > "$d/a.txt"; commit "$d"
printf '\x00\x01binary\x00' > "$d/say\"hi\".bin"; git -C "$d" add -A
                                                                  capture "$d" binary_quoted_path
