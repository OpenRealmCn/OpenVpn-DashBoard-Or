package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"openvpntools/internal/dnsguard"
	"openvpntools/internal/platform"
)

type CheckResult struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type PrecheckReport struct {
	OK            bool          `json:"ok"`
	Checks        []CheckResult `json:"checks"`
	SuggestedAddr string        `json:"suggestedAddr"`
	Installed     bool          `json:"installed"`
}

// RunPrecheck 做全部只读检查;params 需已 Normalize。
func RunPrecheck(ctx context.Context, plat platform.Platform, dns *dnsguard.Guard, p Params, simulate bool) PrecheckReport {
	rep := PrecheckReport{OK: true}
	add := func(name string, ok bool, detail string) {
		rep.Checks = append(rep.Checks, CheckResult{Name: name, OK: ok, Detail: detail})
		if !ok {
			rep.OK = false
		}
	}

	// 已安装检测
	rep.Installed = plat.FS().Exists(filepath.Join(plat.Paths().ServerConfDir, "server.conf"))
	add("未安装过 OpenVPN", !rep.Installed, map[bool]string{
		true: "检测到 server.conf 已存在,本工具只支持全新安装", false: "干净环境",
	}[rep.Installed])

	// 操作系统
	osi, err := plat.OSInfo(ctx)
	switch {
	case err != nil:
		add("操作系统", false, "无法读取 /etc/os-release: "+err.Error())
	case osi.ID == "ubuntu" || osi.ID == "debian":
		add("操作系统", true, osi.Pretty)
	default:
		add("操作系统", false, fmt.Sprintf("%s 不在支持范围(仅 Ubuntu 20.04+ / Debian 11+)", osi.Pretty))
	}

	// root 权限
	if simulate || runtime.GOOS != "linux" {
		add("root 权限", true, "mock 模式跳过")
	} else if os.Geteuid() == 0 {
		add("root 权限", true, "以 root 运行")
	} else {
		add("root 权限", false, "面板未以 root 运行,无法执行安装")
	}

	// TUN 设备
	if simulate {
		add("TUN 设备", true, "mock 模式跳过")
	} else if plat.FS().Exists("/dev/net/tun") {
		add("TUN 设备", true, "/dev/net/tun 可用")
	} else {
		add("TUN 设备", false, "/dev/net/tun 不存在(容器环境需宿主开启)")
	}

	// bash 可用(EasyRSA 依赖)
	if res, err := plat.Run(ctx, platform.RunOpt{Argv: []string{"bash", "--version"}, Timeout: 10 * time.Second}); err != nil || res.ExitCode != 0 {
		add("bash", false, "bash 不可用,EasyRSA 无法执行")
	} else {
		add("bash", true, "可用")
	}

	// 端口占用
	ports, err := plat.ListenPorts(ctx)
	if err != nil {
		add("端口检查", false, "无法枚举监听端口: "+err.Error())
	} else {
		conflict := ""
		for _, pt := range ports {
			if pt.Proto == p.Proto && pt.Port == p.Port {
				conflict = fmt.Sprintf("%s:%d 已被 %s(PID %d)占用", pt.Addr, pt.Port, pt.Comm, pt.PID)
				break
			}
		}
		add(fmt.Sprintf("端口 %s/%d", p.Proto, p.Port), conflict == "", map[bool]string{true: "空闲", false: conflict}[conflict == ""])
	}

	// IPv6 前提检查
	if p.EnableIPv6 {
		if simulate {
			add("IPv6", true, "mock 模式跳过")
		} else if data, err := os.ReadFile("/proc/net/if_inet6"); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			add("IPv6", true, "宿主机具备 IPv6 协议栈")
		} else {
			add("IPv6", false, "宿主机没有可用的 IPv6 协议栈,请关闭 IPv6 选项")
		}
	}

	// self 模式的 resolved / UDP 53 检查
	if p.DNSMode == DNSSelf {
		precheckSelfDNS(ctx, plat, dns, &rep, add)
	}

	rep.SuggestedAddr = detectPublicAddr(ctx, plat, simulate)
	return rep
}

// precheckSelfDNS 校验"本机 resolved 服务 VPN 客户端"模式的前提。
// 硬性规则:未知进程占用 UDP 53 时绝不自动停止,只报告详情。
func precheckSelfDNS(ctx context.Context, plat platform.Platform, dns *dnsguard.Guard, rep *PrecheckReport, add func(string, bool, string)) {
	svc, err := plat.ServiceStatus(ctx, dnsguard.ResolvedUnit)
	if err != nil || !svc.Exists {
		add("systemd-resolved", false, "self 模式需要 systemd-resolved,当前系统没有")
		return
	}
	add("systemd-resolved", true, "存在")

	// DNSStubListenerExtra 需要 systemd >= 247(Ubuntu 20.04 的 245 不支持)
	if res, err := plat.Run(ctx, platform.RunOpt{Argv: []string{"systemctl", "--version"}, Timeout: 10 * time.Second}); err == nil && res.ExitCode == 0 {
		fields := strings.Fields(strings.SplitN(res.Stdout, "\n", 2)[0])
		ver := 0
		if len(fields) >= 2 {
			ver, _ = strconv.Atoi(fields[1])
		}
		if ver > 0 && ver < 247 {
			add("systemd 版本", false, fmt.Sprintf("systemd %d 不支持 DNSStubListenerExtra(需 ≥ 247),请换其它 DNS 模式", ver))
		} else {
			add("systemd 版本", true, fmt.Sprintf("systemd %d", ver))
		}
	}

	rep53, err := dns.CheckPort53(ctx)
	if err != nil {
		add("UDP 53", false, "检查失败: "+err.Error())
		return
	}
	if rep53.Free {
		add("UDP 53", true, "空闲")
		return
	}
	for _, oc := range rep53.Occupants {
		switch oc.Class {
		case dnsguard.ClassResolved:
			// stub 只监听 127.0.0.53,与 VPN 网关地址不冲突
			add("UDP 53", true, fmt.Sprintf("systemd-resolved stub 占用 %s(不冲突)", oc.Addr))
		case dnsguard.ClassOpenVPN:
			add("UDP 53", false, "OpenVPN 实例正在监听 53 端口,该 DNS 模式要求 53 空闲,请更换 VPN 端口或 DNS 模式")
		case dnsguard.ClassKnownDNS:
			add("UDP 53", false, fmt.Sprintf(
				"已知 DNS 服务 %s(PID %d, %s)占用 %s:53;请自行调整其监听或换 DNS 模式,本工具不会代为停止",
				oc.Comm, oc.PID, oc.Unit, oc.Addr))
		default:
			add("UDP 53", false, fmt.Sprintf(
				"未知进程 %s(PID %d)占用 %s:53;为安全起见本工具绝不自动停止未知服务,请人工确认后处理或换 DNS 模式",
				oc.Comm, oc.PID, oc.Addr))
		}
	}
}

// detectPublicAddr 通过默认路由源地址猜测公网地址(NAT 后仍需用户确认)。
func detectPublicAddr(ctx context.Context, plat platform.Platform, simulate bool) string {
	if simulate {
		return "203.0.113.10"
	}
	res, err := plat.Run(ctx, platform.RunOpt{Argv: []string{"ip", "-4", "route", "get", "1.1.1.1"}, Timeout: 10 * time.Second})
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	fields := strings.Fields(res.Stdout)
	for i, f := range fields {
		if f == "src" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func stepPrecheck() Step {
	return Step{ID: "precheck", Name: "环境预检", Run: func(c *StepCtx) error {
		rep := RunPrecheck(c.Ctx, c.Plat, c.DNS, c.Params, c.Simulate)
		for _, chk := range rep.Checks {
			mark := "✓"
			if !chk.OK {
				mark = "✗"
			}
			c.Log("%s %s:%s", mark, chk.Name, chk.Detail)
		}
		if !rep.OK {
			return errors.New("预检未通过,已中止(详见上方日志)")
		}
		return nil
	}}
}
