#!/bin/sh
# scripts/test.sh [FILE.gzl...]
#
# Checks formatting, vets and unit-tests this repository with the pinned Go,
# then verifies each built package: its manifest and CHECKSUMS, static ARM64
# executables, the pinned Tailscale build, and a license file for every
# component linked into its programs.
set -eu
. "$(dirname "$0")/toolchain.sh"

(
	cd "$root"
	unformatted=$(gofmt -l cmd tools)
	[ -z "$unformatted" ] || die "not gofmt-formatted: $unformatted"
	go vet ./...
	go test ./...
	for script in scripts/*.sh packaging/tailscale; do
		sh -n "$script"
	done
)
if [ $# -gt 0 ]; then
	"$gzl" verify -toml "$root/gizlet.toml" -upstream "$root/upstream.env" "$@"
fi
