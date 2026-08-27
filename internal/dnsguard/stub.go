package dnsguard

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"openvpntools/internal/platform"
	"openvpntools/internal/store"
)

type State struct {
	ResolvedExists   bool         `json:"resolvedExists"`
	ResolvedActive   bool         `json:"resolvedActive"`
	DropInPresent    bool         `json:"dropInPresent"`
	ResolvConfIsLink bool         `json:"resolvConfIsLink"`
	ResolvConfTarget string       `json:"resolvConfTarget"`
	BackupPresent    bool         `json:"backupPresent"`
	Port53           Port53Report `json:"port53"`
}

func (g *Guard) State(ctx context.Context) (State, error) {
	var st State
	if svc, err := g.plat.ServiceStatus(ctx, ResolvedUnit); err == nil {
		st.ResolvedExists, st.ResolvedActive = svc.Exists, svc.Active
	}
	paths := g.plat.Paths()
	st.DropInPresent = g.plat.FS().Exists(filepath.Join(paths.ResolvedDropDir, DropInName))
	if target, err := g.plat.FS().Readlink(paths.ResolvConf); err == nil {
		st.ResolvConfIsLink, st.ResolvConfTarget = true, target
	}
	st.BackupPresent = g.plat.FS().Exists(g.backupPath())
	rep, err := g.CheckPort53(ctx)
	if err != nil {
		return st, err
	}
	st.Port53 = rep
	return st, nil
}

// StubBackup 记录关闭 stub 前的原状,足以完整恢复。
type StubBackup struct {
	HadDropIn          bool      `json:"hadDropIn"`
	OldDropIn          []byte    `json:"oldDropIn,omitempty"`
	ResolvConfIsLink   bool      `json:"resolvConfIsLink"`
	OldTarget          string    `json:"oldTarget,omitempty"`
	SwitchedResolvConf bool      `json:"switchedResolvConf"`
	At                 time.Time `json:"at"`
}

func (g *Guard) backupPath() string {
	return filepath.Join(g.plat.Paths().DataDir, backupFile)
}

// SnapshotStub 在动手前记录 drop-in 与 /etc/resolv.conf 的原状。
func (g *Guard) SnapshotStub() StubBackup {
	b := StubBackup{At: time.Now()}
	fs := g.plat.FS()
	dropIn := filepath.Join(g.plat.Paths().ResolvedDropDir, DropInName)
	if fs.Exists(dropIn) {
		b.HadDropIn = true
		if data, err := fs.ReadFile(dropIn); err == nil {
			b.OldDropIn = data
		}
	}
	if target, err := fs.Readlink(g.plat.Paths().ResolvConf); err == nil {
		b.ResolvConfIsLink, b.OldTarget = true, target
	}
	return b
}

// DisableStub 写 drop-in 关闭 DNSStubListener,必要时把 /etc/resolv.conf
// 从 stub 配置切换到真实上游,然后重启 systemd-resolved。
// 绝不改写 /etc/systemd/resolved.conf 主配置。
// 执行过的动作会记入 b(SwitchedResolvConf),供 RevertStub 恢复。
func (g *Guard) DisableStub(ctx context.Context, b *StubBackup) error {
	svc, err := g.plat.ServiceStatus(ctx, ResolvedUnit)
	if err != nil {
		return fmt.Errorf("查询 systemd-resolved 状态失败: %w", err)
	}
	if !svc.Exists {
		return errors.New("系统没有 systemd-resolved,无需处理 DNS Stub")
	}

	fs := g.plat.FS()
	paths := g.plat.Paths()
	if err := fs.MkdirAll(paths.ResolvedDropDir, 0o755); err != nil {
		return err
	}
	dropIn := filepath.Join(paths.ResolvedDropDir, DropInName)
	if err := fs.WriteFileAtomic(dropIn, []byte(dropInBody), 0o644); err != nil {
		return fmt.Errorf("写入 resolved drop-in 失败: %w", err)
	}
	// stub 关闭后 127.0.0.53 不再监听:若 resolv.conf 还指向 stub 配置,
	// 切换到 resolved 维护的真实上游文件,否则本机解析会断。
	if b.ResolvConfIsLink && strings.Contains(b.OldTarget, "stub-resolv.conf") {
		if err := fs.Remove(paths.ResolvConf); err != nil {
			return fmt.Errorf("移除 resolv.conf 软链失败: %w", err)
		}
		if err := fs.Symlink(runtimeResolvConf, paths.ResolvConf); err != nil {
			return fmt.Errorf("切换 resolv.conf 软链失败: %w", err)
		}
		b.SwitchedResolvConf = true
	}
	if err := g.plat.ServiceCtl(ctx, ResolvedUnit, platform.ActRestart); err != nil {
		return fmt.Errorf("重启 systemd-resolved 失败: %w", err)
	}
	return nil
}

// ApplyExtraListener 写 drop-in 让 resolved 额外监听 ip:53(服务 VPN 客户端),
// 同样绝不改写主配置;原状由调用方先用 SnapshotStub 记录。
func (g *Guard) ApplyExtraListener(ctx context.Context, ip string, b *StubBackup) error {
	svc, err := g.plat.ServiceStatus(ctx, ResolvedUnit)
	if err != nil {
		return fmt.Errorf("查询 systemd-resolved 状态失败: %w", err)
	}
	if !svc.Exists {
		return errors.New("系统没有 systemd-resolved,无法使用 self DNS 模式")
	}
	fs := g.plat.FS()
	paths := g.plat.Paths()
	if err := fs.MkdirAll(paths.ResolvedDropDir, 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf(
		"# 由 OpenVpnTools 生成:让 systemd-resolved 为 VPN 客户端提供 DNS\n# 删除本文件并重启 systemd-resolved 即可恢复原状\n[Resolve]\nDNSStubListenerExtra=%s\n",
		ip)
	if err := fs.WriteFileAtomic(filepath.Join(paths.ResolvedDropDir, DropInName), []byte(body), 0o644); err != nil {
		return fmt.Errorf("写入 resolved drop-in 失败: %w", err)
	}
	if err := g.plat.ServiceCtl(ctx, ResolvedUnit, platform.ActRestart); err != nil {
		return fmt.Errorf("重启 systemd-resolved 失败: %w", err)
	}
	return nil
}

// RevertStub 按备份恢复 drop-in 与 resolv.conf,并重启 resolved;实现为幂等。
func (g *Guard) RevertStub(ctx context.Context, b StubBackup) error {
	fs := g.plat.FS()
	paths := g.plat.Paths()
	dropIn := filepath.Join(paths.ResolvedDropDir, DropInName)

	if b.HadDropIn && len(b.OldDropIn) > 0 {
		if err := fs.WriteFileAtomic(dropIn, b.OldDropIn, 0o644); err != nil {
			return err
		}
	} else if fs.Exists(dropIn) {
		if err := fs.Remove(dropIn); err != nil {
			return err
		}
	}
	if b.SwitchedResolvConf {
		target := b.OldTarget
		if target == "" {
			target = stubResolvConf
		}
		_ = fs.Remove(paths.ResolvConf)
		if err := fs.Symlink(target, paths.ResolvConf); err != nil {
			return fmt.Errorf("恢复 resolv.conf 软链失败: %w", err)
		}
	}
	if svc, err := g.plat.ServiceStatus(ctx, ResolvedUnit); err == nil && svc.Exists {
		return g.plat.ServiceCtl(ctx, ResolvedUnit, platform.ActRestart)
	}
	return nil
}

// SaveBackup/LoadBackup/ClearBackup 供独立操作(非安装流程)持久化恢复点;
// 安装器则把 StubBackup 存进自己的回滚 journal。
func (g *Guard) SaveBackup(b StubBackup) error { return store.SaveJSON(g.backupPath(), b) }

func (g *Guard) LoadBackup() (StubBackup, error) {
	var b StubBackup
	err := store.LoadJSON(g.backupPath(), &b)
	return b, err
}

func (g *Guard) ClearBackup() error {
	if g.plat.FS().Exists(g.backupPath()) {
		return g.plat.FS().Remove(g.backupPath())
	}
	return nil
}
