#!/usr/bin/env bash
# OpenVpn-DashBoard-Or 管理脚本:安装 / 更新 / 卸载 / 状态(Debian/Ubuntu,需 root)
#
# 一键安装或更新(无参数自动判断):
#   curl -fsSL https://raw.githubusercontent.com/OpenRealmCn/OpenVpn-DashBoard-Or/main/install.sh | sudo bash
# 子命令:
#   ... | sudo bash -s -- install [vX.Y.Z]      安装(或指定版本)
#   ... | sudo bash -s -- update  [vX.Y.Z]      更新面板二进制(配置与数据不动)
#   ... | sudo bash -s -- uninstall --yes       卸载面板(保留数据目录与 OpenVPN)
#   ... | sudo bash -s -- uninstall --purge --all --yes
#                                               连面板数据、OpenVPN 部署(证书/规则/DNS drop-in)一并清理
#   ... | sudo bash -s -- status                查看状态
#   ... | sudo bash -s -- join <主节点URL> <绑定码> [本节点URL]
#                                               安装(如未装)并绑定到主节点,成为其子节点
# 安装完成后本机可直接使用管理菜单:
#   sudo ovpn-ctl
# 镜像下载(校验和仍优先直连 GitHub):
#   GH_MIRROR=https://your-mirror.example ... | sudo GH_MIRROR=... bash
set -euo pipefail

REPO="OpenRealmCn/OpenVpn-DashBoard-Or"
RAW_SELF="https://raw.githubusercontent.com/$REPO/main/install.sh"
GH="https://github.com"
GH_MIRROR="${GH_MIRROR:-}"

BIN=/usr/local/bin/ovpn-web
CTL=/usr/local/bin/ovpn-ctl
UNIT=/etc/systemd/system/ovpn-web.service
CONF_DIR=/etc/openvpntools
DATA_DIR=/var/lib/openvpntools
SERVER_DIR=/etc/openvpn/server
EASYRSA_DIR=/etc/openvpn/easy-rsa

# 日志一律走 stderr,保证函数经 $(…) 捕获 stdout 时提示仍可见
info() { printf '\033[1;32m[ovpn-web]\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[1;33m[ovpn-web]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[ovpn-web]\033[0m %s\n' "$*" >&2; exit 1; }

require_env() {
    [ "$(id -u)" = 0 ] || die "请以 root 运行(sudo bash)"
    command -v systemctl >/dev/null 2>&1 || die "需要 systemd"
    command -v curl >/dev/null 2>&1 || die "缺少 curl,请先: apt-get install -y curl"
    command -v sha256sum >/dev/null 2>&1 || die "缺少 sha256sum(coreutils)"
}

# confirm <提示>:交互终端里询问;非交互必须带 --yes
ASSUME_YES=0
confirm() {
    [ "$ASSUME_YES" = 1 ] && return 0
    local a
    if read -rp "$1 [y/N] " a </dev/tty 2>/dev/null; then
        case "$a" in y | Y | yes | YES) return 0 ;; *) return 1 ;; esac
    fi
    die "非交互环境执行卸载请追加 --yes"
}

# 全局临时目录,退出时统一清理
# (不可用 RETURN trap:函数返回后 local 变量已销毁,set -u 下后续函数返回会触发 unbound variable)
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

# TLS ≥ 1.2、证书校验、退避重试
fetch() {
    curl -fL --proto '=https' --tlsv1.2 --retry 4 --retry-delay 2 \
        --connect-timeout 15 -o "$2" "$1"
}

detect_arch() {
    case "$(uname -m)" in
        x86_64 | amd64) echo amd64 ;;
        aarch64 | arm64) echo arm64 ;;
        *) die "不支持的架构: $(uname -m)(仅 amd64 / arm64)" ;;
    esac
}

# download_verified <version> <目标临时目录> → 输出二进制路径
download_verified() {
    local version="$1" tmp="$2" arch asset base
    arch="$(detect_arch)"
    asset="ovpn-web-linux-$arch"
    if [ "$version" = "latest" ]; then
        base="$GH/$REPO/releases/latest/download"
    else
        base="$GH/$REPO/releases/download/$version"
    fi

    info "下载 $asset($version)…"
    if [ -n "$GH_MIRROR" ]; then
        fetch "${GH_MIRROR%/}/$base/$asset" "$tmp/$asset" || die "经镜像下载失败"
    else
        fetch "$base/$asset" "$tmp/$asset" || die "下载失败(网络受限可设置 GH_MIRROR 镜像)"
    fi

    info "获取 SHA256SUMS(优先直连 GitHub,防镜像篡改)…"
    if fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS" 2>/dev/null; then
        :
    elif [ -n "$GH_MIRROR" ] && fetch "${GH_MIRROR%/}/$base/SHA256SUMS" "$tmp/SHA256SUMS"; then
        warn "校验和经镜像获取:请确保该镜像可信"
    else
        die "无法获取 SHA256SUMS,拒绝安装"
    fi
    # 兼容文本(两空格)与二进制(" *")两种 sha256sum 行格式
    (cd "$tmp" && grep -E "^[0-9a-f]{64} [ *]$asset\$" SHA256SUMS | sha256sum -c - >&2) \
        || die "SHA256 校验失败,已中止(文件可能损坏或被篡改)"
    info "SHA256 校验通过"
    echo "$tmp/$asset"
}

write_unit() {
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
}

# 把本脚本装成 ovpn-ctl,便于日后本机管理(尽力而为)
install_ctl() {
    if [ -f "${BASH_SOURCE[0]:-}" ] && [ "${BASH_SOURCE[0]:-}" != "/dev/stdin" ]; then
        cp "${BASH_SOURCE[0]}" "$CTL" 2>/dev/null || return 0
    else
        fetch "$RAW_SELF" "$CTL" 2>/dev/null || { warn "ovpn-ctl 自安装失败(不影响面板使用)"; return 0; }
    fi
    chmod 0755 "$CTL"
    info "本机管理命令已就绪: sudo ovpn-ctl"
}

panel_url_hint() {
    local ip
    ip="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}' || true)"
    info "  面板地址: http://${ip:-<服务器IP>}:8686"
}

cmd_install() {
    local version="${1:-latest}" bin_path
    require_env
    if [ -r /etc/os-release ]; then . /etc/os-release; fi
    case "${ID:-}" in
        ubuntu | debian) ;;
        *) warn "未识别的发行版「${ID:-unknown}」:面板可以运行,但安装 OpenVPN 仅支持 Debian/Ubuntu 系" ;;
    esac

    bin_path="$(download_verified "$version" "$TMP_ROOT" | tail -n 1)"

    if [ -f "$BIN" ]; then
        info "检测到已安装,执行更新(配置与数据不受影响)…"
        systemctl stop ovpn-web 2>/dev/null || true
    fi
    install -m 0755 "$bin_path" "$BIN"
    [ -f "$UNIT" ] || { info "写入 systemd 服务 ovpn-web.service …"; write_unit; }
    mkdir -p "$CONF_DIR" "$DATA_DIR"
    systemctl daemon-reload
    systemctl enable --now ovpn-web
    install_ctl

    echo
    info "完成!"
    panel_url_hint
    info "  首次访问请设置管理员密码(≥ 8 位)"
    info "  建议: 用防火墙限制面板端口来源,或在「面板设置」开启 HTTPS"
    info "  管理: sudo ovpn-ctl   或   systemctl status|restart ovpn-web"
}

cmd_update() {
    [ -f "$BIN" ] || die "尚未安装,请先执行 install"
    cmd_install "${1:-latest}"
}

# 按 install.json 尽力清理 OpenVPN 部署(证书、规则、DNS drop-in、sysctl)
teardown_openvpn() {
    info "拆除 OpenVPN 部署 …"
    systemctl disable --now openvpn-server@server 2>/dev/null || true

    local params="$DATA_DIR/install.json" subnet="" subnet6="" proto="" port="" nic=""
    if [ -f "$params" ]; then
        subnet="$(grep -o '"subnet"[^,}]*' "$params" | sed 's/.*: *"\{0,1\}\([^",}]*\).*/\1/' || true)"
        subnet6="$(grep -o '"subnet6"[^,}]*' "$params" | sed 's/.*: *"\{0,1\}\([^",}]*\).*/\1/' || true)"
        proto="$(grep -o '"proto"[^,}]*' "$params" | sed 's/.*: *"\{0,1\}\([^",}]*\).*/\1/' || true)"
        port="$(grep -o '"port"[^,}]*' "$params" | sed 's/.*: *\([0-9]*\).*/\1/' || true)"
        nic="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="dev"){print $(i+1); exit}}' || true)"
    fi
    if [ -n "$subnet" ] && [ -n "$nic" ]; then
        info "清理 iptables 规则($subnet → $nic)…"
        iptables -t nat -D POSTROUTING -s "$subnet" -o "$nic" -j MASQUERADE 2>/dev/null || true
        [ -n "$proto" ] && [ -n "$port" ] && iptables -D INPUT -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || true
        iptables -D FORWARD -s "$subnet" -j ACCEPT 2>/dev/null || true
        iptables -D FORWARD -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
        if [ -n "$subnet6" ]; then
            ip6tables -t nat -D POSTROUTING -s "$subnet6" -o "$nic" -j MASQUERADE 2>/dev/null || true
            [ -n "$proto" ] && [ -n "$port" ] && ip6tables -D INPUT -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || true
            ip6tables -D FORWARD -s "$subnet6" -j ACCEPT 2>/dev/null || true
            ip6tables -D FORWARD -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
        fi
        netfilter-persistent save 2>/dev/null || true
    else
        warn "缺少 install.json 或无法探测网卡,跳过防火墙规则清理(如有请手工检查 iptables)"
    fi

    rm -f "$SERVER_DIR"/server.conf "$SERVER_DIR"/ca.crt "$SERVER_DIR"/server.crt \
        "$SERVER_DIR"/server.key "$SERVER_DIR"/ta.key "$SERVER_DIR"/crl.pem "$SERVER_DIR"/ipp.txt
    rm -rf "$EASYRSA_DIR"
    rm -f /etc/sysctl.d/99-openvpntools.conf /etc/sysctl.d/99-openvpntools-v6.conf
    sysctl --system >/dev/null 2>&1 || true
    if [ -f /etc/systemd/resolved.conf.d/99-openvpntools.conf ]; then
        rm -f /etc/systemd/resolved.conf.d/99-openvpntools.conf
        systemctl restart systemd-resolved 2>/dev/null || true
        info "已移除 resolved drop-in 并重启 systemd-resolved"
    fi
    rm -f /var/log/openvpn/status.log
    info "OpenVPN 部署已拆除;软件包保留,如需卸载: apt-get remove -y openvpn"
}

cmd_uninstall() {
    local purge=0 all=0
    for a in "$@"; do
        case "$a" in
            --purge) purge=1 ;;
            --all) all=1 ;;
            --yes | -y) ASSUME_YES=1 ;;
            *) die "未知参数: $a(可用 --purge --all --yes)" ;;
        esac
    done
    require_env
    [ -f "$BIN" ] || [ -f "$UNIT" ] || warn "看起来面板未安装,继续尝试清理残留"

    local scope="面板(二进制 + systemd 服务)"
    [ "$purge" = 1 ] && scope="$scope + 面板数据($CONF_DIR、$DATA_DIR)"
    [ "$all" = 1 ] && scope="$scope + OpenVPN 部署(服务/证书/防火墙规则/DNS drop-in)"
    confirm "确认卸载: $scope ?" || die "已取消"

    [ "$all" = 1 ] && teardown_openvpn

    info "停止并移除面板 …"
    systemctl disable --now ovpn-web 2>/dev/null || true
    rm -f "$UNIT" "$BIN" "$BIN.old" "$BIN.new" "$CTL"
    systemctl daemon-reload

    if [ "$purge" = 1 ]; then
        rm -rf "$CONF_DIR" "$DATA_DIR"
        info "面板数据已删除"
    else
        info "面板数据保留在 $CONF_DIR 与 $DATA_DIR(重装后继续可用;删除请加 --purge)"
    fi
    [ "$all" = 1 ] || info "OpenVPN 未受影响(连拆 OpenVPN 请加 --all)"
    info "卸载完成"
}

# cmd_join <主节点URL> <绑定码> [本节点URL]:快捷绑定为子节点
cmd_join() {
    local master="${1:-}" code="${2:-}" self_url="${3:-}"
    { [ -n "$master" ] && [ -n "$code" ]; } || die "用法: join <主节点URL> <绑定码> [本节点URL]"
    require_env
    [ -f "$BIN" ] || cmd_install latest

    info "生成本节点接入令牌(写入面板配置)…"
    local token
    token="$("$BIN" -config "$CONF_DIR/config.yaml" -gen-node-token | tail -n 1)"
    [ -n "$token" ] || die "令牌生成失败"
    systemctl restart ovpn-web
    sleep 2

    if [ -z "$self_url" ]; then
        local ip port
        ip="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}' || true)"
        port="$(awk '/^listen:/ {print $2}' "$CONF_DIR/config.yaml" 2>/dev/null | sed 's/.*://' || true)"
        [ -n "$ip" ] || die "无法探测本机地址,请以第三个参数显式提供本节点 URL"
        self_url="http://$ip:${port:-8686}"
    fi

    local host payload resp http_code
    host="$(hostname | tr -cd 'A-Za-z0-9._-')"
    payload="$(printf '{"code":"%s","name":"%s","url":"%s","token":"%s"}' \
        "$code" "${host:-node}" "$self_url" "$token")"
    info "向主节点注册: $master(本节点 $self_url)…"
    resp="$(curl -sS --connect-timeout 15 --retry 2 -w '\n%{http_code}' \
        -X POST -H 'Content-Type: application/json' -d "$payload" \
        "${master%/}/api/nodes/register" 2>&1 || true)"
    http_code="$(printf '%s' "$resp" | tail -n 1)"
    if [ "$http_code" = "200" ]; then
        info "绑定成功!已成为主节点的子节点,可在主节点「节点管理」页看到本机"
    else
        printf '%s\n' "$resp" | head -n -1 >&2
        die "绑定失败(HTTP ${http_code:-?}):请确认绑定码有效、且主节点能访问 $self_url"
    fi
}

cmd_status() {
    require_env
    if [ -f "$BIN" ]; then
        info "面板二进制: $BIN"
    else
        warn "面板未安装"
    fi
    systemctl status ovpn-web --no-pager -l 2>/dev/null | head -n 5 || true
    systemctl status openvpn-server@server --no-pager -l 2>/dev/null | head -n 3 || true
    panel_url_hint
}

menu() {
    echo "OpenVpn-DashBoard-Or 管理"
    echo "  1) 安装 / 更新到最新版"
    echo "  2) 卸载面板(保留数据与 OpenVPN)"
    echo "  3) 卸载面板 + 数据 + OpenVPN 部署"
    echo "  4) 查看状态"
    echo "  0) 退出"
    local c
    read -rp "请选择: " c </dev/tty
    case "$c" in
        1) cmd_install latest ;;
        2) cmd_uninstall ;;
        3) cmd_uninstall --purge --all ;;
        4) cmd_status ;;
        0) exit 0 ;;
        *) die "无效选择" ;;
    esac
}

main() {
    local cmd="${1:-}"
    [ $# -gt 0 ] && shift
    case "$cmd" in
        install) cmd_install "${1:-latest}" ;;
        update) cmd_update "${1:-latest}" ;;
        uninstall) cmd_uninstall "$@" ;;
        join) cmd_join "$@" ;;
        status) cmd_status ;;
        menu) menu ;;
        "")
            # 无参数:交互终端进菜单;管道(curl | bash)按安装/更新处理
            if [ -t 0 ]; then
                menu
            else
                cmd_install latest
            fi
            ;;
        *) die "未知命令: $cmd(可用 install / update / uninstall / join / status / menu)" ;;
    esac
}

main "$@"
