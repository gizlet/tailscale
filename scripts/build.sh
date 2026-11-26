#!/bin/sh
# scripts/build.sh [--clean-cache] [OUT]
#
# Builds the Tailscale service package for Gizlet OS (aarch64-unknown-linux-gnu,
# lite) into OUT (dist by default). Needs only sh, curl, git and tar: it fetches
# the Go toolchain and the Tailscale source pinned in upstream.env, verifies
# both, and builds static, trimmed binaries, so every run on any Linux or macOS
# host produces the same .gzl byte for byte. --clean-cache builds with an empty
# Go build cache. GIZLET_CACHE chooses the download cache (.cache by default).
set -eu

clean_cache=false
if [ "${1:-}" = "--clean-cache" ]; then
	clean_cache=true
	shift
fi
out=${1:-dist}
case $out in /*) ;; *) out=$PWD/$out ;; esac
. "$(dirname "$0")/toolchain.sh"
mkdir -p "$out"

# The Tailscale source at the pinned tag, which must be the pinned commit. The
# local tag lets Go record the module version in the build information.
src=$cache/tailscale-$TAILSCALE_VERSION
if [ ! -d "$src/.git" ]; then
	rm -rf "$src.part"
	git init -q "$src.part"
	git -C "$src.part" fetch -q --depth 1 "$TAILSCALE_REPO" "refs/tags/v$TAILSCALE_VERSION:refs/tags/v$TAILSCALE_VERSION"
	git -C "$src.part" -c advice.detachedHead=false checkout -q "v$TAILSCALE_VERSION"
	mv "$src.part" "$src"
fi
[ "$(git -C "$src" rev-parse HEAD)" = "$TAILSCALE_COMMIT" ] || die "Tailscale v$TAILSCALE_VERSION is not commit $TAILSCALE_COMMIT"
[ -z "$(git -C "$src" status --porcelain --untracked-files=all --ignored)" ] || die "the Tailscale source in $src is modified or has extra files; delete it to fetch it again"
[ "$(cat "$src/VERSION.txt")" = "$TAILSCALE_VERSION" ] || die "Tailscale VERSION.txt is not $TAILSCALE_VERSION"

stage=$(mktemp -d)
if $clean_cache; then
	GOCACHE=$(mktemp -d)
	export GOCACHE
fi
trap 'rm -rf "$stage"; if $clean_cache; then chmod -R u+w "$GOCACHE"; rm -rf "$GOCACHE"; fi' EXIT

cd "$root"
mkdir -p "$stage/bin"

# tailscaled with the CLI linked in, as build_dist.sh --box builds it, stamped
# with the version mkversion derives from the pinned tag and commit. Only the
# Linux system tray is left out: a handheld has none, and it would link the
# FreeType-licensed golang/freetype.
short=$(printf %s "$TAILSCALE_COMMIT" | cut -c1-9)
(
	cd "$src"
	export GOOS=linux GOARCH=arm64
	export TS_VERSION_LONG="$TAILSCALE_VERSION-t$short" TS_VERSION_GIT_HASH="$TAILSCALE_COMMIT"
	TAGS=ts_include_cli,ts_omit_systray ./build_dist.sh --strip -o "$stage/bin/tailscaled" ./cmd/tailscaled
)

# The service entry and the CLI wrapper from this repository.
version=$("$gzl" field gizlet.toml version)
GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false \
	-ldflags "-s -w -X main.packageVersion=$version -X main.tailscaleVersion=$TAILSCALE_VERSION" \
	-o "$stage/bin/tailscale-service" ./cmd/tailscale-service
cp packaging/tailscale "$stage/bin/tailscale"

# License texts for this package, Go, Tailscale and every module linked into
# either binary, read from their build information.
"$gzl" licenses -out "$stage/licenses" -goroot "$goroot" -modcache "$GOMODCACHE" \
	-module "tailscale.com=$src" -module "github.com/gizlet/tailscale=$root" \
	"$stage/bin/tailscaled" "$stage/bin/tailscale-service"

"$gzl" pack -toml gizlet.toml -stage "$stage" -out "$out"
