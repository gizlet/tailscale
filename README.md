# Tailscale for Gizlet OS

The open-source [Tailscale](https://github.com/tailscale/tailscale) client, packaged as a Gizlet service for Gizlet OS handhelds such as the R36S. Once it is installed and signed in, the handheld is a device on your own tailnet: reach it by its Tailscale name or address for `ssh`, `scp` and Taildrop from anywhere, without opening router ports.

It is an unofficial build, not made or supported by Tailscale Inc. Store page: [gizlet.io/github.com/gizlet/tailscale](https://gizlet.io/github.com/gizlet/tailscale).

| | |
| --- | --- |
| Gizlet ID | `io.gizlet.tailscale`, kind `service` (manifest schema 2) |
| Package | `io.gizlet.tailscale-<version>-aarch64-unknown-linux-gnu-lite.gzl` |
| Contains | Tailscale 1.104.7 (pinned in [upstream.env](upstream.env)), static ARM64 |
| Needs | A Gizlet OS whose `gizlet` command installs services (`gizlet service ...`); v0.3.2 and earlier refuse them |

## Installing

Turn on Wi-Fi and SSH in the launcher's **Settings** (SSH takes the keys in `authorized_keys` on the card's boot partition), then log in from your computer. `gizlet-a1b2` stands for your handheld's name, shown in Settings.

```sh
ssh root@gizlet-a1b2.local
curl -fLo /tmp/tailscale.gzl https://dl.gizlet.io/github.com/gizlet/tailscale/v0.1.0/io.gizlet.tailscale-0.1.0-aarch64-unknown-linux-gnu-lite.gzl
gizlet install /tmp/tailscale.gzl
```

`gizlet install` starts the service and sets it to start at boot; it is ready to sign in once the command returns. The package is also attached to each [GitHub release](https://github.com/gizlet/tailscale/releases); copy it over with `scp` and install it the same way. Installing a newer version replaces the package, keeps the sign-in and the boot setting, and restarts the service if it was running.

## Starting and stopping

| Command | Does |
| --- | --- |
| `gizlet service start io.gizlet.tailscale` | Starts it now |
| `gizlet service stop io.gizlet.tailscale` | Stops it; the handheld goes offline in your tailnet |
| `gizlet service disable io.gizlet.tailscale` | Stops starting it at boot (`enable` turns that back on) |
| `gizlet service status io.gizlet.tailscale` | Shows whether it runs |
| `gizlet service logs io.gizlet.tailscale [-f]` | Shows tailscaled's log |

The launcher's **Settings → Services** page has the same **Running** and **Start at boot** switches. The service runs in the background as a system unit: it keeps running while games and apps have the screen and while the launcher restarts. Wi-Fi drops while the handheld sleeps, so it shows as offline in the tailnet until it wakes and Wi-Fi reconnects.

## Signing in

The package's CLI is `/opt/gizlet/services/io.gizlet.tailscale/bin/tailscale`; it talks to this service's daemon. The examples call it `tailscale`:

```sh
alias tailscale=/opt/gizlet/services/io.gizlet.tailscale/bin/tailscale
tailscale up --qr
```

`tailscale up` prints a sign-in link (and with `--qr`, a QR code). Open it on your phone or computer and sign in to your own Tailscale account; the handheld joins that tailnet under its hostname. Then:

```sh
tailscale status        # the tailnet's devices
tailscale ip -4         # this handheld's Tailscale address
```

From another device on the tailnet, `ssh root@gizlet-a1b2` (with MagicDNS) or `ssh root@100.x.y.z` reaches the handheld's own sshd, so SSH must stay on in Settings. To use [Tailscale SSH](https://tailscale.com/kb/1193/tailscale-ssh) instead, which works with SSH off and follows your tailnet's access rules, sign in with `tailscale up --ssh`.

To join without a browser, create an auth key in your Tailscale admin console and pass it once: `tailscale up --auth-key=tskey-...`. Treat it as a password, and keep it out of shared shell history. Your account, keys and enrollment stay yours: the package contains none, and nothing is sent to gizlet.

Tailscale runs in userspace-networking mode, as the R36S kernel has no TUN device. Incoming connections from the tailnet to the handheld work, and so do `tailscale ping`, `tailscale file` and Tailscale SSH. Programs on the handheld do not get a route to other tailnet devices; use `tailscale nc HOST PORT` for that. The service changes no routes, firewall rules or DNS settings.

## Signing out and uninstalling

```sh
tailscale logout                               # removes the handheld from your tailnet
gizlet uninstall io.gizlet.tailscale
```

`gizlet uninstall` stops the service and everything it started, removes its unit and boot setting, and deletes the package, its state (including the node key) and its runtime folder. Without `tailscale logout` first, the handheld stays listed in your admin console as offline until you remove it there.

## Paths

| Path | Holds |
| --- | --- |
| `/opt/gizlet/services/io.gizlet.tailscale/` | The package: `bin/tailscale-service` (the entry), `bin/tailscaled` (daemon and CLI), `bin/tailscale` (CLI wrapper), `licenses/` |
| `/opt/gizlet/service-data/io.gizlet.tailscale/` | Its state (`GIZLET_STATE_DIR`): `tailscaled.state` with the node and machine keys, buffered logs, Taildrop files |
| `/run/gizlet-services/io.gizlet.tailscale/tailscaled.sock` | The LocalAPI socket, in `GIZLET_RUNTIME_DIR`, gone when the service stops |

`bin/tailscale-service` replaces itself with `tailscaled --tun=userspace-networking --statedir=$GIZLET_STATE_DIR --socket=$GIZLET_RUNTIME_DIR/tailscaled.sock`, with `TS_LOGS_DIR` set to the state folder, so the service manager's stop reaches tailscaled directly and it shuts down cleanly. Outside Gizlet OS it falls back to systemd's `STATE_DIRECTORY` and `RUNTIME_DIRECTORY`. [Entry](cmd/tailscale-service/main.go)

## Building

Run on Linux or macOS with `sh`, `curl`, `git` and `tar`:

```sh
./scripts/build.sh                 # dist/io.gizlet.tailscale-<version>-aarch64-unknown-linux-gnu-lite.gzl
./scripts/test.sh dist/*.gzl       # gofmt, go vet, unit tests, package verification
sudo ./scripts/smoke-test.sh dist/*.gzl   # ARM64 Linux with systemd only
```

`build.sh` downloads Go 1.27.4 and checks its SHA-256, fetches Tailscale's tag and checks that it is the pinned commit, and builds both programs static (`CGO_ENABLED=0`) with `-trimpath`. Go modules are checked against `go.sum` and Go's checksum database. `tools/gzl` writes `manifest.json` from [gizlet.toml](gizlet.toml), copies the license and notice files of every module in the programs' Go build information (failing on a missing or unrecognized license), writes `CHECKSUMS`, and zips everything in byte order with fixed timestamps and modes. The same commit therefore builds the same package byte for byte on any host; CI builds it twice, once with an empty build cache, and compares. Downloads are cached in `.cache` (`GIZLET_CACHE` moves it).

`test.sh` verifies a package as the Store and device read it, and that `bin/tailscaled` is the pinned tag and commit, built with the pinned Go for linux/arm64 with the expected tags, and that every linked component has its license files. `smoke-test.sh` installs the package under a systemd unit like the one Gizlet OS writes and checks that it starts, that the CLI reaches it, that it gets a sign-in link from Tailscale's control server (never opened), that a stop is clean and leaves no process, socket, network change or file outside its state, and that the state survives a restart.

To update Tailscale, change the version, commit and, if its `go.mod` asks for a newer Go, the Go version and checksums in `upstream.env`, update the version in `NOTICE.md`, and bump `version` in `gizlet.toml`.

## Releasing

Pull requests run `CI` (build, rebuild and compare, tests, systemd smoke test on GitHub's ARM64 runner) and `Codex Code Review` from gizlet-bot's `/ci` and `/codex-review` comments. A merge to `main` that changes `version` in `gizlet.toml` builds the package again and the shared [Store release workflow](https://github.com/gizlet/.github#releasing-to-gizlet-store) tags `v<version>`, publishes the GitHub release with the `.gzl`, submits it to the Gizlet Store and waits until the Store reports it `ready`.

## Key files

| Path | Role |
| --- | --- |
| [gizlet.toml](gizlet.toml) | The Store description and package declaration |
| [upstream.env](upstream.env) | Pinned Tailscale and Go |
| [cmd/tailscale-service](cmd/tailscale-service/main.go) | The service entry and CLI connection |
| [packaging/tailscale](packaging/tailscale) | The CLI wrapper |
| [tools/gzl](tools/gzl) | Manifest, licenses, packing and verification |
| [scripts](scripts) | Build, test and smoke test |
| [NOTICE.md](NOTICE.md) | What the package contains and its licenses; shipped in the package |
| [store](store) | Icon and the Store page's About text |

## License

This repository is under the [BSD 3-Clause License](LICENSE). The package also contains Tailscale (BSD-3-Clause), Go and the Go modules Tailscale uses, each under its own license; see [NOTICE.md](NOTICE.md) and the package's `licenses/` folder.
