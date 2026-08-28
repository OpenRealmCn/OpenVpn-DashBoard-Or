package installer

import (
	"fmt"
	"regexp"
	"time"

	"openvpntools/internal/platform"
)

var ovpnVerRe = regexp.MustCompile(`OpenVPN (\d+\.\d+)`)

func stepPackages() Step {
	return Step{ID: "pkgs", Name: "安装系统软件包", Run: func(c *StepCtx) error {
		aptEnv := []string{"DEBIAN_FRONTEND=noninteractive"}
		if c.Simulate {
			c.Log("(mock) 跳过 apt-get update")
		} else {
			c.Log("apt-get update …")
			if _, err := runLogged(c, aptEnv, 10*time.Minute, platform.AptGet("update")...); err != nil {
				return err
			}
		}

		var toInstall []string
		for _, pkg := range []string{"openvpn", "iptables-persistent"} {
			installed, err := c.Plat.IsPkgInstalled(c.Ctx, pkg)
			if err != nil {
				return fmt.Errorf("查询软件包 %s: %w", pkg, err)
			}
			if installed {
				c.Log("%s 已安装,跳过", pkg)
			} else {
				toInstall = append(toInstall, pkg)
			}
		}
		if len(toInstall) > 0 {
			// write-ahead:先记 journal 再安装,回滚时只卸载本次新装的包
			if err := c.Journal.Record(c.StepID, ActPkgInstalled, PkgPayload{Pkgs: toInstall}); err != nil {
				return err
			}
			c.Log("apt-get install %v …", toInstall)
			if err := c.Plat.AptInstall(c.Ctx, toInstall...); err != nil {
				return err
			}
		}

		// 探测 OpenVPN 版本(2.4 与 2.5+ 的配置指令有差异)
		if c.Simulate {
			c.Data.OpenVPNVer = "2.6"
		} else {
			// 注意:openvpn --version 的退出码历来是 1,只看输出
			res, err := c.Plat.Run(c.Ctx, RunOptArgv("openvpn", "--version"))
			if err != nil {
				return fmt.Errorf("openvpn --version: %w", err)
			}
			if m := ovpnVerRe.FindStringSubmatch(res.Stdout); m != nil {
				c.Data.OpenVPNVer = m[1]
			} else {
				c.Data.OpenVPNVer = "unknown"
			}
		}
		c.Log("OpenVPN 版本: %s(modern=%v)", c.Data.OpenVPNVer, versionModern(c.Data.OpenVPNVer))
		return nil
	}}
}
