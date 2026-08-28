// Package dnsguard 处理两件事:
//  1. systemd-resolved 的 DNS Stub:通过 drop-in 关闭(绝不改写主配置),支持完整恢复;
//  2. UDP 53 占用检测与归类:resolved / 已知 DNS 服务 / 未知进程。
//     硬性规则:对未知进程绝不提供停止或杀死的能力。
package dnsguard

import (
	"context"
	"strings"

	"openvpntools/internal/platform"
)

const (
	// DropInName 写入 /etc/systemd/resolved.conf.d/ 的文件名。
	DropInName   = "99-openvpntools.conf"
	ResolvedUnit = "systemd-resolved.service"

	runtimeResolvConf = "/run/systemd/resolve/resolv.conf"
	stubResolvConf    = "/run/systemd/resolve/stub-resolv.conf"
	backupFile        = "dnsguard-backup.json"
)

const dropInBody = `# 由 OpenVpnTools 生成:关闭 systemd-resolved 的 DNS Stub 监听
# 删除本文件并重启 systemd-resolved 即可恢复原状
[Resolve]
DNSStubListener=no
`

type Class string

const (
	ClassResolved Class = "resolved"  // systemd-resolved,可由本工具自动处理
	ClassOpenVPN  Class = "openvpn"   // 本面板管理的 OpenVPN 实例(VPN 端口选了 53 的场景)
	ClassKnownDNS Class = "known-dns" // 已知 DNS 服务,仅提示用户自行决策
	ClassUnknown  Class = "unknown"   // 未知进程,绝不自动操作
)

var knownDNSComms = map[string]bool{
	"dnsmasq":     true,
	"unbound":     true,
	"named":       true,
	"coredns":     true,
	"pdns_server": true,
	"pihole-FTL":  true,
}

type Occupant struct {
	platform.PortInfo
	Class Class `json:"class"`
}

type Port53Report struct {
	Free      bool       `json:"free"`
	Occupants []Occupant `json:"occupants"`
}

type Guard struct {
	plat platform.Platform
}

func New(p platform.Platform) *Guard { return &Guard{plat: p} }

// Classify 归类一个监听 53 端口的进程。
func Classify(p platform.PortInfo) Class {
	comm := strings.ToLower(p.Comm)
	switch {
	case p.Unit == ResolvedUnit || strings.HasPrefix(comm, "systemd-resolve"):
		return ClassResolved
	case comm == "openvpn" || strings.HasPrefix(p.Unit, "openvpn-server@"):
		return ClassOpenVPN
	case knownDNSComms[comm] || knownDNSComms[p.Comm]:
		return ClassKnownDNS
	default:
		return ClassUnknown
	}
}

// CheckPort53 检查 UDP 53 的占用情况并归类。
func (g *Guard) CheckPort53(ctx context.Context) (Port53Report, error) {
	ports, err := g.plat.ListenPorts(ctx)
	if err != nil {
		return Port53Report{}, err
	}
	rep := Port53Report{Free: true}
	for _, p := range ports {
		if p.Proto == "udp" && p.Port == 53 {
			rep.Free = false
			rep.Occupants = append(rep.Occupants, Occupant{PortInfo: p, Class: Classify(p)})
		}
	}
	return rep, nil
}
