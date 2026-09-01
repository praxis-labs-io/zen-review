#!/bin/sh
# Hold the turn open while the zen-review session has comments nobody has
# answered yet.
#
# Two vocabularies meet here and neither is the other's. zen-review says 1 for
# "the filter matched" and 2 for "the call failed". Claude Code honours only 2
# as a block, and on Stop a block means keep going, with stderr handed to the
# model as the reason; 1 is a non-blocking error it proceeds past. Wired
# straight through, an open comment would let the agent stop and a broken call
# would trap it, so this script is the translation.
#
# It asks for open comments and not unresolved ones. Unresolved is every state
# but resolved, and resolve is the reader's verb: an agent that answered
# everything leaves it addressed, which is still unresolved, so the gate it just
# cleared would hold it again until the client gave up.

set -u

# No repository, or no review opened here, is not this hook's business. The
# check is what keeps a plugin installed once from opening a review database in
# every repository the agent stops in: zen-review resolves the session, and
# resolving it creates it.
dir=$(git rev-parse --git-common-dir 2>/dev/null) || exit 0
[ -f "$dir/zen-review/state.db" ] || exit 0

out=$(zen-review comments --state open --json --exit-code 2>&1)
case $? in
	0)
		# Nothing open. Let the turn end.
		exit 0
		;;
	1)
		# A queue. Block the stop and hand the listing over as the reason.
		printf '%s\n' "$out" >&2
		printf 'Answer each one with: zen-review address <id> --body "..."\n' >&2
		printf 'Then run: zen-review refresh\n' >&2
		exit 2
		;;
	*)
		# Broken, or not installed. Say so where a person sees it, and never
		# exit 2: a failure that blocks is a failure the agent cannot get past.
		printf 'zen-review could not read the review queue:\n%s\n' "$out" >&2
		exit 1
		;;
esac
