package installer

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"openvpntools/internal/openvpn"
	"openvpntools/internal/platform"
)

func stepSysctl() Step {
	return Step{ID: "sysctl", Name: "开启 IP 转发(sysctl.d)", Run: func(c *StepCtx) error {
		apply := func(file, key string) error {
			old, _ := c.Plat.ReadSysctl(key)
			path := filepath.Join(c.Plat.Paths().SysctlDir, file)
			payload := SysctlPayload{File: file, Key: key, OldRuntime: old}
			if c.Plat.FS().Exists(path) {
				payload.HadFile = true
				if data, err := c.Plat.FS().ReadFile(path); err == nil {
					payload.OldFileB64 = base64.StdEncoding.EncodeToString(data)
				}
			}
			if err := c.Journal.Record(c.StepID, ActSysctlSet, payload); err != nil {
				return err
			}
			if err := c.Plat.WriteSysctlD(file, key, "1"); err != nil {
				return err
			}
			c.Log("%s=1 已写入 /etc/sysctl.d/%s 并即时生效(原运行时值 %q)", key, file, old)
			return nil
		}
		if err := apply("99-openvpntools.conf", "net.ipv4.ip_forward"); err != nil {
			return err
		}
		if c.Params.EnableIPv6 {
			return apply("99-openvpntools-v6.conf", "net.ipv6.conf.all.forwarding")
		}
		return nil
	}}
}

func stepFirewall() Step {
	return Step{ID: "firewall", Name: "配置 NAT 与防火墙规则", Run: func(c *StepCtx) error {
		nic := "eth0"
		if c.Simulate {
			c.Log("(mock) 出网网卡按 eth0 处理")
		} else {
			res, err := runLogged(c, nil, 15*time.Second, "ip", "-4", "route", "get", "1.1.1.1")
			if err != nil {
				return fmt.Errorf("探测出网网卡失败: %w", err)
			}
			fields := strings.Fields(res.Stdout)
			nic = ""
			for i, f := range fields {
				if f == "dev" && i+1 < len(fields) {
					nic = fields[i+1]
					break
				}
			}
			if nic == "" {
				return errors.New("无法从默认路由解析出网网卡")
			}
		}
		c.Data.OutNIC = nic
		c.Log("出网网卡: %s", nic)

		port := strconv.Itoa(c.Params.Port)
		rules := []IptRule{
			{Table: "nat", Chain: "POSTROUTING", Spec: []string{"-s", c.Params.Subnet, "-o", nic, "-j", "MASQUERADE"}},
			{Chain: "INPUT", Spec: []string{"-p", c.Params.Proto, "--dport", port, "-j", "ACCEPT"}},
			{Chain: "FORWARD", Spec: []string{"-s", c.Params.Subnet, "-j", "ACCEPT"}},
			{Chain: "FORWARD", Spec: []string{"-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"}},
		}
		if c.Params.EnableIPv6 {
			rules = append(rules,
				IptRule{V6: true, Table: "nat", Chain: "POSTROUTING", Spec: []string{"-s", c.Params.Subnet6, "-o", nic, "-j", "MASQUERADE"}},
				IptRule{V6: true, Chain: "INPUT", Spec: []string{"-p", c.Params.Proto, "--dport", port, "-j", "ACCEPT"}},
				IptRule{V6: true, Chain: "FORWARD", Spec: []string{"-s", c.Params.Subnet6, "-j", "ACCEPT"}},
				IptRule{V6: true, Chain: "FORWARD", Spec: []string{"-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"}},
			)
		}
		if err := c.Journal.Record(c.StepID, ActIptables, IptablesPayload{Rules: rules}); err != nil {
			return err
		}
		for _, r := range rules {
			argv := []string{r.Binary()}
			if r.Table != "" {
				argv = append(argv, "-t", r.Table)
			}
			argv = append(argv, "-A", r.Chain)
			argv = append(argv, r.Spec...)
			c.Log("%s", strings.Join(argv, " "))
			if _, err := runLogged(c, nil, 30*time.Second, argv...); err != nil {
				return err
			}
		}
		c.Log("netfilter-persistent save …")
		if _, err := runLogged(c, nil, time.Minute, "netfilter-persistent", "save"); err != nil {
			return err
		}
		return nil
	}}
}

func stepService() Step {
	return Step{ID: "service", Name: "启动 OpenVPN 服务", Run: func(c *StepCtx) error {
		st, err := c.Plat.ServiceStatus(c.Ctx, openvpn.ServiceUnit)
		if err != nil {
			return err
		}
		payload := SvcPayload{Unit: openvpn.ServiceUnit, WasActive: st.Active, WasEnabled: st.Enabled}
		if err := c.Journal.Record(c.StepID, ActServiceState, payload); err != nil {
			return err
		}
		c.Log("systemctl enable --now %s", openvpn.ServiceUnit)
		if err := c.Plat.ServiceCtl(c.Ctx, openvpn.ServiceUnit, platform.ActEnableNow); err != nil {
			return err
		}
		deadline := time.Now().Add(15 * time.Second)
		for {
			st, err := c.Plat.ServiceStatus(c.Ctx, openvpn.ServiceUnit)
			if err == nil && st.Active {
				c.Log("服务已进入 active 状态")
				return nil
			}
			if time.Now().After(deadline) {
				return errors.New("服务在 15 秒内未进入 active 状态,请查看 journalctl -u " + openvpn.ServiceUnit)
			}
			select {
			case <-c.Ctx.Done():
				return c.Ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}}
}

func stepDNSSelf() Step {
	return Step{
		ID: "dnsself", Name: "配置 resolved 服务 VPN 客户端(drop-in)",
		Skip: func(c *StepCtx) bool { return c.Params.DNSMode != DNSSelf },
		Run: func(c *StepCtx) error {
			backup := c.DNS.SnapshotStub()
			// write-ahead:先把原状记入 journal,回滚即可完整恢复
			if err := c.Journal.Record(c.StepID, ActDNSStub, backup); err != nil {
				return err
			}
			gw := c.Params.GatewayIP()
			c.Log("写 drop-in:DNSStubListenerExtra=%s(不改写 resolved 主配置)", gw)
			if err := c.DNS.ApplyExtraListener(c.Ctx, gw, &backup); err != nil {
				return err
			}
			// 复检:resolved 应已监听网关 53
			deadline := time.Now().Add(10 * time.Second)
			for {
				rep, err := c.DNS.CheckPort53(c.Ctx)
				if err == nil {
					for _, oc := range rep.Occupants {
						if oc.Class == "resolved" && strings.HasPrefix(oc.Addr, gw) {
							c.Log("resolved 已监听 %s:53", gw)
							return nil
						}
					}
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("resolved 未能监听 %s:53(tun 设备可能未就绪),已中止", gw)
				}
				select {
				case <-c.Ctx.Done():
					return c.Ctx.Err()
				case <-time.After(time.Second):
				}
			}
		},
	}
}

func stepVerify() Step {
	return Step{ID: "verify", Name: "最终验证", Run: func(c *StepCtx) error {
		st, err := c.Plat.ServiceStatus(c.Ctx, openvpn.ServiceUnit)
		if err != nil || !st.Active {
			return errors.New("验证失败: OpenVPN 服务不是 active 状态")
		}
		ports, err := c.Plat.ListenPorts(c.Ctx)
		if err != nil {
			return err
		}
		for _, p := range ports {
			matchPort := p.Port == c.Params.Port && p.Proto == c.Params.Proto
			if matchPort || (c.Simulate && p.Comm == "openvpn") {
				c.Log("验证通过: %s 正在监听 %s:%d", p.Comm, p.Addr, p.Port)
				return nil
			}
		}
		return fmt.Errorf("验证失败: 未发现 %s/%d 的监听", c.Params.Proto, c.Params.Port)
	}}
}
