// Package mock 供 Windows 本地开发与前端联调:不触碰真实系统,
// 服务/端口/sysctl 状态保存在内存,文件操作全部落到 dataDir/mockroot 下。
package mock

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"openvpntools/internal/platform"
)

type Mock struct {
	mu       sync.Mutex
	fs       platform.OSFS
	paths    platform.Paths
	services map[string]*platform.ServiceStatus
	sysctl   map[string]string
	pkgs     map[string]bool
	ports    []platform.PortInfo
}

func New(dataDir string) *Mock {
	root := filepath.Join(dataDir, "mockroot")
	return &Mock{
		paths: platform.Paths{
			DataDir:         dataDir,
			EasyRSADir:      filepath.Join(root, "easy-rsa"),
			PKIDir:          filepath.Join(root, "easy-rsa", "pki"),
			ServerConfDir:   filepath.Join(root, "openvpn-server"),
			SysctlDir:       filepath.Join(root, "sysctl.d"),
			ResolvedDropDir: filepath.Join(root, "resolved.conf.d"),
			ResolvConf:      filepath.Join(root, "resolv.conf"),
		},
		services: map[string]*platform.ServiceStatus{
			"systemd-resolved.service": {Exists: true, Active: true, Enabled: true},
			"ssh.service":              {Exists: true, Active: true, Enabled: true},
		},
		sysctl: map[string]string{"net.ipv4.ip_forward": "0"},
		pkgs:   map[string]bool{},
		ports: []platform.PortInfo{
			{Proto: "udp", Addr: "127.0.0.53%lo", Port: 53, PID: 300, Comm: "systemd-resolve", Unit: "systemd-resolved.service"},
			{Proto: "tcp", Addr: "0.0.0.0", Port: 22, PID: 800, Comm: "sshd", Unit: "ssh.service"},
		},
	}
}

func (m *Mock) FS() platform.FS       { return m.fs }
func (m *Mock) Paths() platform.Paths { return m.paths }

func (m *Mock) OSInfo(ctx context.Context) (platform.OSInfo, error) {
	return platform.OSInfo{ID: "ubuntu", VersionID: "22.04", Pretty: "Ubuntu 22.04 (mock)"}, nil
}

func (m *Mock) ServiceCtl(ctx context.Context, unit string, act platform.ServiceAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.services[unit]
	if !ok {
		st = &platform.ServiceStatus{Exists: true}
		m.services[unit] = st
	}
	switch act {
	case platform.ActStart, platform.ActRestart:
		st.Active = true
	case platform.ActStop:
		st.Active = false
	case platform.ActEnable:
		st.Enabled = true
	case platform.ActDisable:
		st.Enabled = false
	case platform.ActEnableNow:
		st.Active, st.Enabled = true, true
	case platform.ActDisableNow:
		st.Active, st.Enabled = false, false
	case platform.ActDaemonReload:
	}
	m.syncOpenVPNPort(unit, st.Active)
	m.syncResolvedPort(unit, st.Active)
	return nil
}

// syncResolvedPort 模拟 systemd-resolved 重启后的行为:
// drop-in 含 DNSStubListener=no 时 127.0.0.53:53 不再监听;
// 含 DNSStubListenerExtra=<ip> 时额外监听 <ip>:53。
func (m *Mock) syncResolvedPort(unit string, active bool) {
	if unit != "systemd-resolved.service" {
		return
	}
	stubOff := false
	extra := ""
	dropIn := filepath.Join(m.paths.ResolvedDropDir, "99-openvpntools.conf")
	if data, err := m.fs.ReadFile(dropIn); err == nil {
		content := string(data)
		stubOff = strings.Contains(content, "DNSStubListener=no")
		for _, line := range strings.Split(content, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "DNSStubListenerExtra="); ok {
				extra = strings.TrimSpace(v)
			}
		}
	}
	kept := m.ports[:0]
	for _, p := range m.ports {
		if !(p.Unit == unit && p.Port == 53) {
			kept = append(kept, p)
		}
	}
	m.ports = kept
	if active && !stubOff {
		m.ports = append(m.ports, platform.PortInfo{
			Proto: "udp", Addr: "127.0.0.53%lo", Port: 53, PID: 300,
			Comm: "systemd-resolve", Unit: unit,
		})
	}
	if active && extra != "" {
		m.ports = append(m.ports, platform.PortInfo{
			Proto: "udp", Addr: extra, Port: 53, PID: 300,
			Comm: "systemd-resolve", Unit: unit,
		})
	}
}

// syncOpenVPNPort 让 mock 的端口列表跟随 openvpn 服务状态,方便状态页联调。
func (m *Mock) syncOpenVPNPort(unit string, active bool) {
	if !strings.HasPrefix(unit, "openvpn-server@") {
		return
	}
	kept := m.ports[:0]
	for _, p := range m.ports {
		if p.Comm != "openvpn" {
			kept = append(kept, p)
		}
	}
	m.ports = kept
	if active {
		m.ports = append(m.ports, platform.PortInfo{
			Proto: "udp", Addr: "0.0.0.0", Port: 1194, PID: 1900, Comm: "openvpn", Unit: unit,
		})
	}
}

func (m *Mock) ServiceStatus(ctx context.Context, unit string) (platform.ServiceStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.services[unit]; ok {
		return *st, nil
	}
	return platform.ServiceStatus{}, nil
}

func (m *Mock) ListenPorts(ctx context.Context) ([]platform.PortInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]platform.PortInfo, len(m.ports))
	copy(out, m.ports)
	return out, nil
}

func (m *Mock) ReadSysctl(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.sysctl[key]; ok {
		return v, nil
	}
	return "0", nil
}

func (m *Mock) WriteSysctlD(file, key, value string) error {
	m.mu.Lock()
	m.sysctl[key] = value
	m.mu.Unlock()
	path := filepath.Join(m.paths.SysctlDir, file)
	content := fmt.Sprintf("# 由 OpenVpnTools 生成(mock)\n%s = %s\n", key, value)
	return m.fs.WriteFileAtomic(path, []byte(content), 0o644)
}

func (m *Mock) AptInstall(ctx context.Context, pkgs ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range pkgs {
		m.pkgs[p] = true
	}
	return nil
}

func (m *Mock) IsPkgInstalled(ctx context.Context, pkg string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pkgs[pkg], nil
}

// Run 返回成功的空结果并回显命令,便于在 SSE 日志里看到 mock 行为。
func (m *Mock) Run(ctx context.Context, opt platform.RunOpt) (platform.RunResult, error) {
	return platform.RunResult{
		ExitCode: 0,
		Stdout:   "(mock) " + strings.Join(opt.Argv, " ") + "\n",
	}, nil
}
