// Package linux 是 platform.Platform 的生产实现,
// 通过 systemctl / ss / apt-get / dpkg-query / sysctl 操作 Debian/Ubuntu 系统。
package linux

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openvpntools/internal/platform"
)

type Linux struct {
	fs    platform.OSFS
	paths platform.Paths
}

func New(dataDir string) *Linux {
	return &Linux{paths: platform.Paths{
		DataDir:         dataDir,
		EasyRSADir:      "/etc/openvpn/easy-rsa",
		PKIDir:          "/etc/openvpn/easy-rsa/pki",
		ServerConfDir:   "/etc/openvpn/server",
		SysctlDir:       "/etc/sysctl.d",
		ResolvedDropDir: "/etc/systemd/resolved.conf.d",
		ResolvConf:      "/etc/resolv.conf",
	}}
}

func (l *Linux) FS() platform.FS       { return l.fs }
func (l *Linux) Paths() platform.Paths { return l.paths }

func (l *Linux) Run(ctx context.Context, opt platform.RunOpt) (platform.RunResult, error) {
	return platform.ExecRun(ctx, opt)
}

func (l *Linux) OSInfo(ctx context.Context) (platform.OSInfo, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return platform.OSInfo{}, err
	}
	var info platform.OSInfo
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"`)
		switch k {
		case "ID":
			info.ID = v
		case "VERSION_ID":
			info.VersionID = v
		case "PRETTY_NAME":
			info.Pretty = v
		}
	}
	return info, nil
}

func (l *Linux) ServiceCtl(ctx context.Context, unit string, act platform.ServiceAction) error {
	var argv []string
	switch act {
	case platform.ActEnableNow:
		argv = []string{"systemctl", "enable", "--now", unit}
	case platform.ActDisableNow:
		argv = []string{"systemctl", "disable", "--now", unit}
	case platform.ActDaemonReload:
		argv = []string{"systemctl", "daemon-reload"}
	default:
		argv = []string{"systemctl", string(act), unit}
	}
	res, err := l.Run(ctx, platform.RunOpt{Argv: argv, Timeout: 2 * time.Minute})
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %w", act, unit, err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("systemctl %s %s: %s", act, unit, tail(res.Stderr, 400))
	}
	return nil
}

func (l *Linux) ServiceStatus(ctx context.Context, unit string) (platform.ServiceStatus, error) {
	res, err := l.Run(ctx, platform.RunOpt{
		Argv:    []string{"systemctl", "show", unit, "--property=LoadState,ActiveState,UnitFileState"},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return platform.ServiceStatus{}, err
	}
	var st platform.ServiceStatus
	for _, line := range strings.Split(res.Stdout, "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch k {
		case "LoadState":
			st.Exists = v == "loaded"
		case "ActiveState":
			st.Active = v == "active"
		case "UnitFileState":
			st.Enabled = v == "enabled"
		}
	}
	return st, nil
}

func (l *Linux) ReadSysctl(key string) (string, error) {
	p := "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (l *Linux) WriteSysctlD(file, key, value string) error {
	path := filepath.Join(l.paths.SysctlDir, file)
	content := fmt.Sprintf("# 由 OpenVpnTools 生成,请勿手工修改\n%s = %s\n", key, value)
	if err := l.fs.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return err
	}
	res, err := l.Run(context.Background(), platform.RunOpt{
		Argv: []string{"sysctl", "--system"}, Timeout: time.Minute,
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("sysctl --system: %s", tail(res.Stderr, 400))
	}
	return nil
}

func (l *Linux) AptInstall(ctx context.Context, pkgs ...string) error {
	argv := append([]string{"apt-get", "install", "-y", "--no-install-recommends"}, pkgs...)
	res, err := l.Run(ctx, platform.RunOpt{
		Argv:    argv,
		Env:     []string{"DEBIAN_FRONTEND=noninteractive"},
		Timeout: 15 * time.Minute,
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("apt-get install %s: %s", strings.Join(pkgs, " "), tail(res.Stderr, 800))
	}
	return nil
}

func (l *Linux) IsPkgInstalled(ctx context.Context, pkg string) (bool, error) {
	res, err := l.Run(ctx, platform.RunOpt{
		Argv:    []string{"dpkg-query", "-W", "-f=${Status}", pkg},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0 && strings.Contains(res.Stdout, "install ok installed"), nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
