#!/bin/sh
# Build zen-review from source and install it.
#
# There is no release artifact yet, so this clones and builds. It needs a Go
# toolchain: it saves you the clone, not the dependency.
#
#   curl -fsSL https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.sh | sh
#
# INSTALL_DIR overrides where the binary lands. It defaults to ~/.local/bin.

set -eu

REPO="https://github.com/praxis-labs-io/zen-review.git"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY="zen-review"

for tool in git go; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "$tool is not on PATH, and building zen-review needs it." >&2
		exit 1
	fi
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM

echo "Cloning $REPO"
git clone --depth 1 --quiet "$REPO" "$work/zen-review"

echo "Building $BINARY"
mkdir -p "$INSTALL_DIR"
(cd "$work/zen-review" && go build -o "$INSTALL_DIR/$BINARY" ./cmd/zen-review)

echo "Installed $INSTALL_DIR/$BINARY"

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	echo
	echo "$INSTALL_DIR is not on your PATH. Add it:"
	echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
	;;
esac
