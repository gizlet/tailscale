#!/bin/sh
# scripts/smoke-test.sh FILE.gzl
#
# Runs the packaged service on this ARM64 Linux machine as Gizlet OS runs it:
# unpacked under services/<id>, started by a systemd unit like the one
# `gizlet install` writes, with GIZLET_STATE_DIR and a RuntimeDirectory under
# /run/gizlet-services. Checks that it starts, that bin/tailscale reaches it,
# that it gets a sign-in URL from the Tailscale control server (which is never
# opened, so nothing joins a tailnet), that a stop shuts tailscaled down
# cleanly, that its state survives a restart, and that it leaves no network
# configuration, process or file behind. Needs root, systemd and jq.
set -eu

[ $# -eq 1 ] || { echo "usage: $0 FILE.gzl" >&2; exit 2; }
[ "$(id -u)" = 0 ] || { echo "smoke-test.sh needs root" >&2; exit 2; }
[ "$(uname -s)/$(uname -m)" = Linux/aarch64 ] || { echo "smoke-test.sh needs ARM64 Linux" >&2; exit 2; }
package=$(readlink -f "$1")

id=io.gizlet.tailscale
unit=gizlet-service-$id.service
[ ! -e "/run/systemd/system/$unit" ] || { echo "$unit already exists; is Tailscale installed here?" >&2; exit 2; }
# Not under /tmp, which PrivateTmp= hides from the unit, nor /opt/gizlet, which a real device uses.
mkdir -p /opt
work=$(mktemp -d /opt/gizlet-smoke.XXXXXX)
install=$work/services/$id
state=$work/service-data/$id
runtime=/run/gizlet-services/$id
socket=$runtime/tailscaled.sock

fail() {
	echo "FAIL: $*" >&2
	journalctl --no-pager -u "$unit" -n 80 >&2 || true
	exit 1
}
step() { echo "== $*"; }

cleanup() {
	systemctl stop "$unit" 2>/dev/null || true
	rm -f "/run/systemd/system/$unit"
	systemctl daemon-reload
	rm -rf "$work"
}
trap cleanup EXIT

network_snapshot() {
	ip -o link | sed 's/^[0-9]*: //; s/ state .*//' | sort
	ip rule
	ip -6 rule
	ip route show table all | grep -v '^\(local\|broadcast\|multicast\) ' | grep -v ' proto kernel ' || true
	if command -v iptables-save >/dev/null; then iptables-save | grep -v '^#' | sed 's/\[[0-9:]*\]//'; fi
	if command -v nft >/dev/null; then nft list ruleset 2>/dev/null | grep -v '^#' || true; fi
	sha256sum /etc/resolv.conf
	readlink -f /etc/resolv.conf
}

leftover_processes() {
	for exe in /proc/[0-9]*/exe; do
		target=$(readlink "$exe" 2>/dev/null) || continue
		case $target in "$install"/*) echo "${exe%/exe}: $target" ;; esac
	done
}

step "install $package"
mkdir -p "$install" "$(dirname "$state")"
unzip -q "$package" -d "$install"
(cd "$install" && sha256sum --quiet -c CHECKSUMS)
[ "$(jq -r '.schema, .kind, .entry' "$install/manifest.json" | tr '\n' ' ')" = "2 service bin/tailscale-service " ] || fail "manifest.json"
"$install/bin/tailscale-service" --version

before=$work/network-before
network_snapshot >"$before"

step "start"
cat >"/run/systemd/system/$unit" <<UNIT
[Unit]
Description=Gizlet service $id (smoke test)
After=network.target

[Service]
Type=exec
ExecStart=$install/bin/tailscale-service
WorkingDirectory=$install
Environment=GIZLET_SERVICE_ID=$id GIZLET_SERVICE_DIR=$install GIZLET_STATE_DIR=$state GIZLET_RUNTIME_DIR=$runtime HOME=$state
Restart=on-failure
RestartSec=2
TimeoutStopSec=15
KillMode=control-group
RuntimeDirectory=gizlet-services/$id
RuntimeDirectoryMode=0700
PrivateTmp=yes
StandardInput=null
UNIT
systemctl daemon-reload
systemctl start "$unit"

wait_for_socket() {
	i=0
	until [ -S "$socket" ]; do
		i=$((i + 1))
		[ $i -le 60 ] || fail "no socket at $socket"
		sleep 0.5
	done
}
wait_for_socket
pid=$(systemctl show -p MainPID --value "$unit")
[ "$(readlink "/proc/$pid/exe")" = "$install/bin/tailscaled" ] || fail "the unit's main process is not tailscaled"
tr '\0' ' ' <"/proc/$pid/cmdline"
echo

step "CLI on the default socket"
"$install/bin/tailscale" version
i=0
until [ "$("$install/bin/tailscale" status --json | jq -r .BackendState)" = NeedsLogin ]; do
	i=$((i + 1))
	[ $i -le 60 ] || fail "BackendState is not NeedsLogin"
	sleep 0.5
done
echo "BackendState: NeedsLogin"

step "sign-in URL from the control server"
timeout 90 "$install/bin/tailscale" up --timeout=75s >"$work/up.log" 2>&1 &
up=$!
i=0
url=
until [ -n "$url" ]; do
	i=$((i + 1))
	[ $i -le 120 ] || { cat "$work/up.log" >&2; fail "no sign-in URL"; }
	sleep 0.5
	url=$("$install/bin/tailscale" status --json | jq -r '.AuthURL // empty')
done
kill "$up" 2>/dev/null || true
wait "$up" 2>/dev/null || true
case $url in https://login.tailscale.com/a/*) echo "sign-in URL: https://login.tailscale.com/a/… (not opened)" ;; *) fail "unexpected sign-in URL host" ;; esac

step "stop"
start=$(date +%s)
systemctl stop "$unit"
elapsed=$(($(date +%s) - start))
echo "stopped in ${elapsed}s"
[ "$elapsed" -lt 10 ] || fail "stopping took ${elapsed}s"
[ "$(systemctl show -p Result --value "$unit")" = success ] || fail "the unit's result is $(systemctl show -p Result --value "$unit")"
journalctl --no-pager -u "$unit" | grep -q 'tailscaled got signal terminated; shutting down' || fail "tailscaled did not shut down on SIGTERM"
if journalctl --no-pager -u "$unit" | grep -E 'timed out|SIGKILL|panic:'; then fail "unclean stop"; fi
[ ! -e "$runtime" ] || fail "$runtime is left behind"
[ -z "$(leftover_processes)" ] || fail "processes left: $(leftover_processes)"
network_snapshot >"$work/network-after"
diff -u "$before" "$work/network-after" || fail "the network configuration changed"
for path in /var/lib/tailscale /run/tailscale /var/run/tailscale /var/cache/tailscale /root/.cache/Tailscale /root/.local/share/tailscale; do
	[ ! -e "$path" ] || fail "$path was created"
done
[ -s "$state/tailscaled.state" ] || fail "no state in $state"
echo "state: $(cd "$state" && find . -mindepth 1 -maxdepth 1 | sort | tr '\n' ' ')"

step "restart keeps the state"
key=$(jq -r '._machinekey // empty' "$state/tailscaled.state")
[ -n "$key" ] || fail "no machine key in the state"
systemctl start "$unit"
wait_for_socket
"$install/bin/tailscale" status --json | jq -e '.BackendState' >/dev/null
[ "$(jq -r '._machinekey' "$state/tailscaled.state")" = "$key" ] || fail "the machine key changed"

step "uninstall"
systemctl stop "$unit"
rm -f "/run/systemd/system/$unit"
systemctl daemon-reload
rm -rf "$install" "$state"
[ ! -e "$runtime" ] || fail "$runtime is left behind"
[ -z "$(leftover_processes)" ] || fail "processes left after uninstall"
network_snapshot >"$work/network-after"
diff -u "$before" "$work/network-after" || fail "the network configuration changed"
echo "smoke test passed"
