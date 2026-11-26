## Your handheld on your tailnet

Tailscale connects your devices into a private network, your *tailnet*, wherever they are. With this service your handheld joins it too: reach it from your laptop or phone by its Tailscale name or address, copy saves and ROMs with `scp`, or open a shell from anywhere, without opening ports on your router.

- **Runs in the background.** It starts with the handheld if you want, keeps running while you play, and has no screen of its own.
- **Your own account.** Sign in with the Tailscale account you already use. Nothing is shared with gizlet.
- **Leaves the system alone.** It uses Tailscale's userspace networking, so it changes no routes, firewall rules or DNS settings, and uninstalling it removes everything it stored.

## Getting started

Turn on Wi-Fi and SSH in **Settings**, then from your computer (with your handheld's name in place of `gizlet-a1b2`):

```sh
ssh root@gizlet-a1b2.local
curl -fLo /tmp/tailscale.gzl https://dl.gizlet.io/github.com/gizlet/tailscale/v0.1.0/io.gizlet.tailscale-0.1.0-aarch64-unknown-linux-gnu-lite.gzl
gizlet install /tmp/tailscale.gzl
gizlet service start io.gizlet.tailscale
/opt/gizlet/services/io.gizlet.tailscale/bin/tailscale up --qr
```

Open the link (or scan the QR code) and sign in. The [guide](https://github.com/gizlet/tailscale#readme) covers starting at boot, auth keys and uninstalling.

This is an unofficial build of the open-source [Tailscale](https://github.com/tailscale/tailscale) client, not made or supported by Tailscale Inc.
