#!/bin/sh
# 在 Debian/Ubuntu 服务器上安装 OpenVpnTools 面板(需 root)。
# 用法: ./install-panel.sh ./ovpn-web-linux-amd64
set -eu

BIN="${1:-./ovpn-web-linux-amd64}"

if [ "$(id -u)" != "0" ]; then
    echo "请以 root 运行" >&2
    exit 1
fi
if [ ! -f "$BIN" ]; then
    echo "找不到二进制文件: $BIN" >&2
    exit 1
fi

install -m 0755 "$BIN" /usr/local/bin/ovpn-web
install -m 0644 "$(dirname "$0")/ovpn-web.service" /etc/systemd/system/ovpn-web.service
mkdir -p /etc/openvpntools /var/lib/openvpntools

systemctl daemon-reload
systemctl enable --now ovpn-web

echo "面板已启动。首次访问 http://<服务器IP>:8686 设置管理员密码。"
echo "建议:面板端口用防火墙限制来源,或在设置页开启 HTTPS。"
