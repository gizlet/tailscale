## 让掌机加入你的 tailnet

Tailscale 把你的设备连成一个私有网络（tailnet），无论它们在哪里。安装这个服务后，掌机也会加入：在电脑或手机上用 Tailscale 名称或地址访问它，用 `scp` 拷贝存档和 ROM，或者随时随地打开终端，无需在路由器上开放端口。

- **在后台运行。** 可以随掌机开机启动，玩游戏时继续运行，没有自己的界面。
- **使用你自己的账号。** 用你已有的 Tailscale 账号登录，不会与 gizlet 共享任何信息。
- **不改动系统。** 它使用 Tailscale 的用户态网络，不修改路由、防火墙规则或 DNS 设置，卸载时会删除它保存的全部内容。

## 开始使用

在 **设置** 中打开 Wi-Fi 和 SSH，然后在电脑上运行（把 `gizlet-a1b2` 换成你的掌机名称）：

```sh
ssh root@gizlet-a1b2.local
curl -fLo /tmp/tailscale.gzl https://dl.gizlet.io/github.com/gizlet/tailscale/v0.1.0/io.gizlet.tailscale-0.1.0-aarch64-unknown-linux-gnu-lite.gzl
gizlet install /tmp/tailscale.gzl
gizlet service start io.gizlet.tailscale
/opt/gizlet/services/io.gizlet.tailscale/bin/tailscale up --qr
```

打开链接（或扫描二维码）并登录。开机启动、认证密钥和卸载等说明见[使用指南](https://github.com/gizlet/tailscale#readme)。

这是开源 [Tailscale](https://github.com/tailscale/tailscale) 客户端的非官方构建，并非由 Tailscale Inc. 制作或提供支持。
