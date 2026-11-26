# Sourced by the other scripts: sets root, cache and the pinned Go toolchain
# for this host (fetched into the cache and checked against upstream.env), and
# exports a Go environment that ignores the user's own Go settings.
# GIZLET_CACHE chooses the cache (.cache by default).

root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/upstream.env"
cache=${GIZLET_CACHE:-$root/.cache}
mkdir -p "$cache"
cache=$(cd "$cache" && pwd)

die() {
	echo "$(basename "$0"): $*" >&2
	exit 1
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

host_os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
x86_64 | amd64) host_arch=amd64 ;;
arm64 | aarch64) host_arch=arm64 ;;
*) die "unsupported host architecture $(uname -m)" ;;
esac
eval "go_sha256=\${GO_SHA256_${host_os}_${host_arch}:-}"
[ -n "$go_sha256" ] || die "no pinned Go toolchain for $host_os/$host_arch"
goroot=$cache/go-$GO_VERSION-$host_os-$host_arch
if [ ! -x "$goroot/bin/go" ]; then
	archive=$cache/go$GO_VERSION.$host_os-$host_arch.tar.gz
	curl -fsSL --retry 3 -o "$archive.part" "https://go.dev/dl/go$GO_VERSION.$host_os-$host_arch.tar.gz"
	[ "$(sha256 "$archive.part")" = "$go_sha256" ] || die "Go archive checksum mismatch"
	rm -rf "$goroot.part"
	mkdir -p "$goroot.part"
	tar -xzf "$archive.part" -C "$goroot.part" --strip-components 1
	rm -f "$archive.part"
	mv "$goroot.part" "$goroot"
fi

export GOROOT="$goroot"
export PATH="$goroot/bin:$PATH"
export GOTOOLCHAIN=local
export GOFLAGS=-mod=readonly
export GOPATH="$cache/gopath"
export GOMODCACHE="$cache/gopath/pkg/mod"
export GOCACHE="$cache/gocache"
export GOENV=off
export GOWORK=off
export CGO_ENABLED=0
unset GOOS GOARCH GOARM64 GOAMD64 GOEXPERIMENT GOFIPS140 GODEBUG GOPROXY GOSUMDB GONOSUMDB GOPRIVATE GOINSECURE 2>/dev/null || true
[ "$(go env GOVERSION)" = "go$GO_VERSION" ] || die "expected go$GO_VERSION, found $(go env GOVERSION)"

# gzl, this repository's package tool, built for the host.
gzl=$cache/bin/gzl-$host_os-$host_arch
(cd "$root" && go build -trimpath -o "$gzl" ./tools/gzl)
