# Notice

This package is an unofficial build of the open-source Tailscale client for Gizlet OS, made by gizlet from <https://github.com/gizlet/tailscale>. It is not made, endorsed or supported by Tailscale Inc. "Tailscale" is a trademark of Tailscale Inc., used here only to name the software the package contains.

| File | What it is | License |
| --- | --- | --- |
| `bin/tailscaled` | Tailscale 1.104.7, `tailscale.com/cmd/tailscaled` with the CLI linked in, built from the unmodified source at tag `v1.104.7` (commit `cbc74fe48ef1b1649f69a4d9a602319427e25102`) of <https://github.com/tailscale/tailscale> with Go 1.27.4, the build tags `ts_include_cli,ts_omit_systray`, and debug symbols stripped | BSD-3-Clause, Copyright (c) 2020 Tailscale Inc & contributors, with Tailscale's patent grant: `modules/tailscale.com/v1.104.7/` |
| `bin/tailscale-service`, `bin/tailscale` | The service entry and CLI wrapper from gizlet/tailscale | BSD-3-Clause, Copyright (c) 2026 gizlet: `github.com/gizlet/tailscale/LICENSE` |
| Both programs | The Go standard library and runtime, Go 1.27.4 | BSD-3-Clause, with Google's patent grant: `go/` |
| `bin/tailscaled` | The Go modules Tailscale depends on, at the versions in its `go.sum` | Their own licenses, under `modules/<module>/<version>/` |

`README.md` beside this file lists every component recorded in the programs' Go build information, the license terms found in its files, and which program uses it. Each component's complete license and notice files are in the folder it names; they, not the summary, are authoritative.

The sources are public: Tailscale at the tag above, each Go module from <https://proxy.golang.org> at the listed version (verified against Go's checksum database), and Go from <https://go.dev/dl/>. Building gizlet/tailscale at this package's release tag with `scripts/build.sh` reproduces the package byte for byte.
