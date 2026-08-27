package installer

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strings"

	"openvpntools/internal/store"
)

type DNSMode string

const (
	DNSCloudflare DNSMode = "cloudflare" // 1.1.1.1 / 1.0.0.1
	DNSGoogle     DNSMode = "google"     // 8.8.8.8 / 8.8.4.4
	DNSSystem     DNSMode = "system"     // 解析本机上游并推送给客户端
	DNSSelf       DNSMode = "self"       // resolved 经 DNSStubListenerExtra 监听 VPN 网关
	DNSCustom     DNSMode = "custom"     // 用户自填
)

type Params struct {
	Port       int     `json:"port"`
	Proto      string  `json:"proto"` // udp / tcp
	Subnet     string  `json:"subnet"`
	EnableIPv6 bool    `json:"enableIPv6"`
	Subnet6    string  `json:"subnet6"` // 默认 fd42:42:42:42::/112(ULA)
	DNSMode    DNSMode `json:"dnsMode"`
	DNS1       string  `json:"dns1"`
	DNS2       string  `json:"dns2"`
	PublicAddr string  `json:"publicAddr"` // 客户端 remote 用的公网 IP 或域名
}

var addrRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)

// Normalize 填充默认值并校验;所有外部输入在此收口。
func (p *Params) Normalize() error {
	if p.Port == 0 {
		p.Port = 1194
	}
	if p.Port < 1 || p.Port > 65535 {
		return fmt.Errorf("端口 %d 不合法", p.Port)
	}
	p.Proto = strings.ToLower(strings.TrimSpace(p.Proto))
	if p.Proto == "" {
		p.Proto = "udp"
	}
	if p.Proto != "udp" && p.Proto != "tcp" {
		return fmt.Errorf("协议必须是 udp 或 tcp")
	}
	if strings.TrimSpace(p.Subnet) == "" {
		p.Subnet = "10.8.0.0/24"
	}
	ip, ipnet, err := net.ParseCIDR(p.Subnet)
	if err != nil || ip.To4() == nil {
		return fmt.Errorf("VPN 网段必须是 IPv4 CIDR,如 10.8.0.0/24")
	}
	ones, _ := ipnet.Mask.Size()
	if ones < 8 || ones > 29 {
		return fmt.Errorf("VPN 网段掩码需在 /8 到 /29 之间")
	}
	if !ip.Equal(ipnet.IP) {
		return fmt.Errorf("VPN 网段必须是网络地址(如 10.8.0.0/24,而不是 10.8.0.1/24)")
	}
	if p.EnableIPv6 {
		if strings.TrimSpace(p.Subnet6) == "" {
			p.Subnet6 = "fd42:42:42:42::/112"
		}
		ip6, ipnet6, err := net.ParseCIDR(p.Subnet6)
		if err != nil || ip6.To4() != nil {
			return fmt.Errorf("IPv6 网段必须是合法的 IPv6 CIDR,如 fd42:42:42:42::/112")
		}
		ones6, _ := ipnet6.Mask.Size()
		if ones6 < 48 || ones6 > 112 {
			return fmt.Errorf("IPv6 网段前缀需在 /48 到 /112 之间")
		}
		if !ip6.Equal(ipnet6.IP) {
			return fmt.Errorf("IPv6 网段必须是网络地址")
		}
	} else {
		p.Subnet6 = ""
	}
	switch p.DNSMode {
	case "", DNSCloudflare:
		p.DNSMode = DNSCloudflare
	case DNSGoogle, DNSSystem, DNSSelf:
	case DNSCustom:
		if net.ParseIP(p.DNS1) == nil {
			return fmt.Errorf("自定义 DNS1 不是合法 IP")
		}
		if p.DNS2 != "" && net.ParseIP(p.DNS2) == nil {
			return fmt.Errorf("自定义 DNS2 不是合法 IP")
		}
	default:
		return fmt.Errorf("未知 DNS 模式: %s", p.DNSMode)
	}
	p.PublicAddr = strings.TrimSpace(p.PublicAddr)
	if p.PublicAddr == "" {
		return fmt.Errorf("请填写服务器公网地址(IP 或域名)")
	}
	if net.ParseIP(p.PublicAddr) == nil && !addrRe.MatchString(p.PublicAddr) {
		return fmt.Errorf("公网地址格式不合法")
	}
	return nil
}

// Network 返回网络地址与掩码(server 指令用),GatewayIP 返回网段第一个主机地址。
func (p *Params) Network() (string, string) {
	_, ipnet, _ := net.ParseCIDR(p.Subnet)
	return ipnet.IP.String(), net.IP(ipnet.Mask).String()
}

func (p *Params) GatewayIP() string {
	_, ipnet, _ := net.ParseCIDR(p.Subnet)
	ip := ipnet.IP.To4()
	gw := net.IPv4(ip[0], ip[1], ip[2], ip[3]+1)
	return gw.String()
}

// Gateway6 返回 IPv6 网段的第一个主机地址(如 fd42:42:42:42::1)。
func (p *Params) Gateway6() string {
	_, ipnet, err := net.ParseCIDR(p.Subnet6)
	if err != nil {
		return ""
	}
	gw := make(net.IP, len(ipnet.IP))
	copy(gw, ipnet.IP)
	gw[len(gw)-1]++
	return gw.String()
}

// —— 安装参数持久化:成功后落盘,供客户端 .ovpn 渲染 remote 等信息 ——

func ParamsPath(dataDir string) string {
	return filepath.Join(dataDir, "install.json")
}

func SaveParams(dataDir string, p Params) error {
	return store.SaveJSON(ParamsPath(dataDir), p)
}

func LoadParams(dataDir string) (Params, error) {
	var p Params
	err := store.LoadJSON(ParamsPath(dataDir), &p)
	return p, err
}
