#!/bin/sh
# Builds ./go-xpra-$VERSION-r$REVISION (with .exe on Windows), the way xpra
# names its builds: $VERSION comes from internal/version/version.go and
# $REVISION is the number of commits since the last tag. The revision is also
# embedded in the binary, so `go-xpra --version` prints the same string.
#
# Extra arguments go to `go build`, e.g. ./build.sh -trimpath
set -eu
cd "$(dirname "$0")"

VERSION=$(sed -n 's/^const Version = "\(.*\)"$/\1/p' internal/version/version.go)
if [ -z "${VERSION}" ]; then
	echo "build.sh: cannot read Version from internal/version/version.go" >&2
	exit 1
fi

# Commits since the last tag; with no tag at all, every commit counts.
if tag=$(git describe --tags --abbrev=0 2>/dev/null); then
	REVISION=$(git rev-list --count "${tag}..HEAD")
else
	REVISION=$(git rev-list --count HEAD)
fi

EXT=$(go env GOEXE)
OUTPUT="go-xpra-${VERSION}-r${REVISION}${EXT}"
go build -ldflags="-X github.com/Xpra-org/go-xpra/internal/version.Revision=${REVISION}" \
	-o "${OUTPUT}" "$@" ./cmd/go-xpra
echo "${OUTPUT}"
