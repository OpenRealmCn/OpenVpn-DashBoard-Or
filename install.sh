#!/usr/bin/env bash
# OpenVpn-DashBoard-Or 一键安装/更新脚本(Debian/Ubuntu,需 root)
#
# 安装最新版:
#   curl -fsSL https://raw.githubusercontent.com/OpenRealmCn/OpenVpn-DashBoard-Or/main/install.sh | sudo bash
# 指定版本:
#   curl -fsSL https://raw.githubusercontent.com/OpenRealmCn/OpenVpn-DashBoard-Or/main/install.sh | sudo bash -s -- v0.3.0
# 使用 GitHub 镜像下载(校验和仍优先直连 GitHub 获取):
#   GH_MIRROR=https://your-mirror.example curl -fsSL .../install.sh | sudo GH_MIRROR=https://your-mirror.example bash
set -euo pipefail

REPO="OpenRealmCn/OpenVpn-DashBoard-Or"
VERSION="${1:-latest}"
BIN=/usr/local/bin/ovpn-web
UNIT=/etc/systemd/system/ovpn-web.service
GH="https://github.com"
GH_MIRROR="${GH_MIRROR:-}"

info() { printf '\033[1;32m[ovpn-web]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[ovpn-web]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[ovpn-web]\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "请以 root 运行(sudo bash)"
command -v systemctl >/dev/null 2>&1 || die "需要 systemd"
command -v curl >/dev/null 2>&1 || die "缺少 curl,请先: apt-get install -y curl"
command -v sha256sum >/dev/null 2>&1 || die "缺少 sha256sum(coreutils)"

if [ -r /etc/os-release ]; then
    . /etc/os-release
fi
case "${ID:-}" in
    ubuntu | debian) ;;
    *) warn "未识别的发行版「${ID:-unknown}」:面板可以运行,但安装 OpenVPN 仅支持 Debian/Ubuntu 系" ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) ARCH=amd64 ;;
    aarch64 | arm64) ARCH=arm64 ;;
    *) die "不支持的架构: $(uname -m)(仅 amd64 / arm64)" ;;
esac
ASSET="ovpn-web-linux-$ARCH"

if [ "$VERSION" = "latest" ]; then
    BASE="$GH/$REPO/releases/latest/download"
else
    BASE="$GH/$REPO/releases/download/$VERSION"
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# TLS ≥ 1.2、证书校验、指数退避重试
fetch() {
    curl -fL --proto '=https' --tlsv1.2 --retry 4 --retry-delay 2 \
        --connect-timeout 15 -o "$2" "$1"
}

info "下载 $ASSET($VERSION)…"
if [ -n "$GH_MIRROR" ]; then
    fetch "${GH_MIRROR%/}/$BASE/$ASSET" "$TMP/$ASSET" || die "经镜像下载失败"
else
    fetch "$BASE/$ASSET" "$TMP/$ASSET" || die "下载失败(网络受限可设置 GH_MIRROR 镜像)"
fi

info "获取 SHA256SUMS(优先直连 GitHub,防镜像篡改)…"
if fetch "$BASE/SHA256SUMS" "$TMP/SHA256SUMS" 2>/dev/null; then
    :
elif [ -n "$GH_MIRROR" ] && fetch "${GH_MIRROR%/}/$BASE/SHA256SUMS" "$TMP/SHA256SUMS"; then
    warn "校验和经镜像获取:请确保该镜像可信"
else
    die "无法获取 SHA256SUMS,拒绝安装"
fi

# 兼容文本(两空格)与二进制(" *")两种 sha256sum 行格式
(cd "$TMP" && grep -E "^[0-9a-f]{64} [ *]$ASSET\$" SHA256SUMS | sha256sum -c -) \
    || die "SHA256 校验失败,已中止(文件可能损坏或被篡改)"
info "SHA256 校验通过"

if [ -f "$BIN" ]; then
    info "检测到已安装,执行更新(配置与数据不受影响)…"
    systemctl stop ovpn-web 2>/dev/null || true
fi
install -m 0755 "$TMP/$ASSET" "$BIN"

if [ ! -f "$UNIT" ]; then
    info "写入 systemd 服务 ovpn-web.service …"
    cat >"$UNIT" <<'EOF'
[Unit]
Description=OpenVpnTools Web Panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
# 面板需要 root:安装软件包、写 /etc、控制 systemd 服务
ExecStart=/usr/local/bin/ovpn-web -config /etc/openvpntools/config.yaml
# always:支持面板自更新后经 /api/panel/restart 干净退出并自动拉起
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
fi

mkdir -p /etc/openvpntools /var/lib/openvpntools
systemctl daemon-reload
systemctl enable --now ovpn-web

IP="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}' || true)"
echo
info "安装完成!"
info "  面板地址: http://${IP:-<服务器IP>}:8686"
info "  首次访问请设置管理员密码(≥ 8 位)"
info "  建议: 用防火墙限制面板端口来源,或在「面板设置」开启 HTTPS"
info "  服务管理: systemctl status|restart ovpn-web"
