package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"openvpntools/internal/openvpn"
)

func stepServerConf() Step {
	return Step{ID: "serverconf", Name: "生成服务端配置", Run: func(c *StepCtx) error {
		fs := c.Plat.FS()
		paths := c.Plat.Paths()

		pushDNS, err := resolvePushDNS(c)
		if err != nil {
			return err
		}
		c.Data.PushDNS = pushDNS
		c.Log("推送给客户端的 DNS: %v", pushDNS)

		// 把 PKI 产物拷入 /etc/openvpn/server(每个文件先记 journal)
		type copyItem struct {
			src, dst string
			perm     uint32
		}
		pki := paths.PKIDir
		items := []copyItem{
			{filepath.Join(pki, "ca.crt"), filepath.Join(paths.ServerConfDir, "ca.crt"), 0o644},
			{filepath.Join(pki, "issued", "server.crt"), filepath.Join(paths.ServerConfDir, "server.crt"), 0o644},
			{filepath.Join(pki, "private", "server.key"), filepath.Join(paths.ServerConfDir, "server.key"), 0o600},
			{filepath.Join(pki, "ta.key"), filepath.Join(paths.ServerConfDir, "ta.key"), 0o600},
			// CRL 需要能被降权后的 openvpn 进程重读
			{filepath.Join(pki, "crl.pem"), filepath.Join(paths.ServerConfDir, "crl.pem"), 0o644},
		}
		if err := fs.MkdirAll(paths.ServerConfDir, 0o755); err != nil {
			return err
		}
		for _, it := range items {
			data, err := fs.ReadFile(it.src)
			if err != nil {
				return fmt.Errorf("读取 %s: %w", it.src, err)
			}
			if err := writeFileJournaled(c, it.dst, data, os.FileMode(it.perm)); err != nil {
				return err
			}
		}

		// 状态日志目录(已连接客户端列表用)
		if !c.Simulate {
			logDir := filepath.Dir(openvpn.StatusLogPath)
			if !fs.Exists(logDir) {
				if err := c.Journal.Record(c.StepID, ActDirCreated, FilePayload{Path: logDir}); err != nil {
					return err
				}
				if err := fs.MkdirAll(logDir, 0o755); err != nil {
					return err
				}
			}
		}

		network, netmask := c.Params.Network()
		conf := openvpn.RenderServerConf(openvpn.ServerConfParams{
			Port:       c.Params.Port,
			Proto:      c.Params.Proto,
			Network:    network,
			Netmask:    netmask,
			EnableIPv6: c.Params.EnableIPv6,
			Subnet6:    c.Params.Subnet6,
			PushDNS:    pushDNS,
			Modern:     versionModern(c.Data.OpenVPNVer),
		})
		confPath := filepath.Join(paths.ServerConfDir, "server.conf")
		if err := writeFileJournaled(c, confPath, []byte(conf), 0o644); err != nil {
			return err
		}
		c.Log("server.conf 已写入: %s", confPath)
		return nil
	}}
}

// resolvePushDNS 按 DNS 模式确定推送地址;system 模式解析本机上游,
// resolv.conf 指向 stub 时读取 resolved 维护的真实上游文件。
func resolvePushDNS(c *StepCtx) ([]string, error) {
	switch c.Params.DNSMode {
	case DNSCloudflare:
		return []string{"1.1.1.1", "1.0.0.1"}, nil
	case DNSGoogle:
		return []string{"8.8.8.8", "8.8.4.4"}, nil
	case DNSCustom:
		out := []string{c.Params.DNS1}
		if c.Params.DNS2 != "" {
			out = append(out, c.Params.DNS2)
		}
		return out, nil
	case DNSSelf:
		return []string{c.Params.GatewayIP()}, nil
	case DNSSystem:
		if c.Simulate {
			return []string{"192.168.1.1"}, nil
		}
		return systemUpstreams(c)
	}
	return nil, fmt.Errorf("未知 DNS 模式: %s", c.Params.DNSMode)
}

func systemUpstreams(c *StepCtx) ([]string, error) {
	fs := c.Plat.FS()
	src := c.Plat.Paths().ResolvConf
	if target, err := fs.Readlink(src); err == nil && strings.Contains(target, "stub-resolv.conf") {
		src = "/run/systemd/resolve/resolv.conf"
	}
	data, err := fs.ReadFile(src)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", src, err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) >= 2 && f[0] == "nameserver" && !strings.HasPrefix(f[1], "127.") && f[1] != "::1" {
			out = append(out, f[1])
			if len(out) == 2 {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("本机没有可用的非回环上游 DNS,请改用公共 DNS 模式")
	}
	return out, nil
}
