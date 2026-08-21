#!/bin/sh
# Install zen-review.
#
# Downloads the binary for this machine from the latest release, and builds from
# source when there is no artifact for it. Only the fallback needs Go.
#
#   curl -fsSL https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.sh | sh
#
# INSTALL_DIR overrides where the binary lands, and defaults to ~/.local/bin.
# VERSION pins a release, as v0.1.0, and defaults to the latest.

set -eu

REPO="praxis-labs-io/zen-review"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY="zen-review"

need() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "$1 is not on PATH, and installing zen-review needs it." >&2
		exit 1
	fi
}

need curl
need tar

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*) arch="" ;;
esac

# The latest release's tag, read out of the API rather than asking for jq.
latest_tag() {
	curl -fsL "https://api.github.com/repos/$REPO/releases/latest" |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
		head -n 1
}

from_release() {
	[ -n "$arch" ] || return 1

	tag="${VERSION:-$(latest_tag)}"
	[ -n "$tag" ] || return 1

	url="https://github.com/$REPO/releases/download/$tag/${BINARY}_${tag#v}_${os}_${arch}.tar.gz"
	echo "Downloading $tag for $os/$arch"
	curl -fsL "$url" -o "$work/release.tar.gz" || return 1
	tar -xzf "$work/release.tar.gz" -C "$work" || return 1
	cp "$work/$BINARY" "$INSTALL_DIR/$BINARY"
	chmod 0755 "$INSTALL_DIR/$BINARY"
}

from_source() {
	echo "No release binary for $os/$arch. Building from source."
	need git
	need go
	git clone --depth 1 --quiet "https://github.com/$REPO.git" "$work/src"
	(cd "$work/src" && go build -o "$INSTALL_DIR/$BINARY" "./cmd/$BINARY")
}

mkdir -p "$INSTALL_DIR"
from_release || from_source

echo "Installed $INSTALL_DIR/$BINARY"

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	echo
	echo "$INSTALL_DIR is not on your PATH. Add it:"
	echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
	;;
esac
