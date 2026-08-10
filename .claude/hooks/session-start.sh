#!/bin/bash
# SessionStart hook: download modules and point git at the tracked hooks so
# `make all` and the pre-push guard work immediately in a fresh checkout.
set -euo pipefail

cd "${CLAUDE_PROJECT_DIR:-.}"

git config core.hooksPath .githooks

# Module download is only worth it in remote (web) containers; local sessions
# already have a warm module cache.
if [ "${CLAUDE_CODE_REMOTE:-}" = "true" ]; then
	go mod download
fi
