package openvpn

import (
	"fmt"
	"strings"
)

// StatusLogPath 供状态页/客户端在线列表解析。
const StatusLogPath = "/var/log/openvpn/status.log"

type ServerConfParams struct {
	Port       int
	Proto      string // udp / tcp
	Network    string // 如 10.8.0.0
	Netmask    string // 如 255.255.255.0
	EnableIPv6 bool
	Subnet6    string // CIDR,如 fd42:42:42:42::/112
	PushDNS    []string
	Modern     bool // OpenVPN >= 2.5:data-ciphers;2.4 用 ncp-ciphers
}

// RenderServerConf 生成 /etc/openvpn/server/server.conf。
// 现代默认:tls-crypt、AEAD 数据通道、dh none + ECDH、CRL 校验。
func RenderServerConf(p ServerConfParams) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w("# 由 OpenVpnTools 生成")
	w("port %d", p.Port)
	w("proto %s", p.Proto)
	w("dev tun")
	w("topology subnet")
	w("server %s %s", p.Network, p.Netmask)
	w("ifconfig-pool-persist ipp.txt")
	if p.EnableIPv6 {
		w("server-ipv6 %s", p.Subnet6)
		w(`push "redirect-gateway def1 ipv6 bypass-dhcp"`)
	} else {
		w(`push "redirect-gateway def1 bypass-dhcp"`)
	}
	for _, dns := range p.PushDNS {
		w(`push "dhcp-option DNS %s"`, dns)
	}
	w("keepalive 10 120")
	w("persist-key")
	w("persist-tun")
	w("user nobody")
	w("group nogroup")
	w("dh none")
	w("ecdh-curve prime256v1")
	w("ca ca.crt")
	w("cert server.crt")
	w("key server.key")
	w("tls-crypt ta.key")
	w("crl-verify crl.pem")
	w("auth SHA256")
	w("cipher AES-256-GCM")
	if p.Modern {
		w("data-ciphers AES-256-GCM:AES-128-GCM")
	} else {
		w("ncp-ciphers AES-256-GCM:AES-128-GCM")
	}
	w("tls-version-min 1.2")
	w("status %s", StatusLogPath)
	w("management %s unix", MgmtSocket)
	w("verb 3")
	if p.Proto == "udp" {
		w("explicit-exit-notify 1")
	}
	return b.String()
}
