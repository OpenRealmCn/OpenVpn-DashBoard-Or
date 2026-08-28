package installer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"openvpntools/internal/dnsguard"
	"openvpntools/internal/platform"
)

// —— journal payload 结构 ——

type FilePayload struct {
	Path string `json:"path"`
}

type FileReplacedPayload struct {
	Path   string `json:"path"`
	OldB64 string `json:"oldB64"`
	Perm   uint32 `json:"perm"`
}

type PkgPayload struct {
	Pkgs []string `json:"pkgs"`
}

type SysctlPayload struct {
	File       string `json:"file"`
	Key        string `json:"key"`
	OldRuntime string `json:"oldRuntime"`
	HadFile    bool   `json:"hadFile"`
	OldFileB64 string `json:"oldFileB64"`
}

type IptRule struct {
	V6    bool     `json:"v6"`    // true = ip6tables
	Table string   `json:"table"` // 空 = filter
	Chain string   `json:"chain"`
	Spec  []string `json:"spec"`
}

func (r IptRule) Binary() string {
	if r.V6 {
		return "ip6tables"
	}
	return "iptables"
}

type IptablesPayload struct {
	Rules []IptRule `json:"rules"`
}

type SvcPayload struct {
	Unit       string `json:"unit"`
	WasActive  bool   `json:"wasActive"`
	WasEnabled bool   `json:"wasEnabled"`
}

// Rollbacker 按 journal 逆序撤销。撤销逻辑按动作类型集中实现,
// 与步骤代码解耦——面板重启后无需重建步骤对象也能回滚。
type Rollbacker struct {
	Plat platform.Platform
	DNS  *dnsguard.Guard
	Log  func(format string, args ...any)
}

// Rollback 逆序撤销全部条目;单条失败记录后继续,返回未复原项。
func (r *Rollbacker) Rollback(ctx context.Context, entries []JournalEntry) []string {
	var failures []string
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		r.Log("回滚 #%d [%s] %s …", e.Seq, e.StepID, e.Action)
		if err := r.undo(ctx, e); err != nil {
			msg := fmt.Sprintf("#%d %s/%s: %v", e.Seq, e.StepID, e.Action, err)
			failures = append(failures, msg)
			r.Log("回滚失败(继续处理其余条目): %s", msg)
		}
	}
	return failures
}

func (r *Rollbacker) undo(ctx context.Context, e JournalEntry) error {
	fs := r.Plat.FS()
	switch e.Action {
	case ActFileCreated:
		var p FilePayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if fs.Exists(p.Path) {
			return fs.Remove(p.Path)
		}
		return nil

	case ActFileReplaced:
		var p FileReplacedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		old, err := base64.StdEncoding.DecodeString(p.OldB64)
		if err != nil {
			return err
		}
		return fs.WriteFileAtomic(p.Path, old, os.FileMode(p.Perm))

	case ActDirCreated:
		var p FilePayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		return fs.RemoveAll(p.Path)

	case ActPkgInstalled:
		var p PkgPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if len(p.Pkgs) == 0 {
			return nil
		}
		env := []string{"DEBIAN_FRONTEND=noninteractive"}
		remove := func() (platform.RunResult, error) {
			return r.Plat.Run(ctx, platform.RunOpt{
				Argv: append(platform.AptGet("remove", "-y"), p.Pkgs...),
				Env:  env, Timeout: 10 * time.Minute,
			})
		}
		res, err := remove()
		if err != nil {
			return err
		}
		if res.ExitCode != 0 {
			// 安装被打断(锁竞争、断电等)可能留下 dpkg 半配置状态,
			// 先修复自己造成的残局再重试一次卸载
			r.Log("apt-get remove 退出码 %d,尝试 dpkg --configure -a 后重试 …", res.ExitCode)
			if cres, cerr := r.Plat.Run(ctx, platform.RunOpt{
				Argv: []string{"dpkg", "--configure", "-a"}, Env: env, Timeout: 10 * time.Minute,
			}); cerr != nil || cres.ExitCode != 0 {
				r.Log("dpkg --configure -a 未成功(继续重试卸载)")
			}
			if res, err = remove(); err != nil {
				return err
			}
			if res.ExitCode != 0 {
				return fmt.Errorf("apt-get remove 退出码 %d: %s", res.ExitCode, tailStr(res.Stderr, 300))
			}
		}
		return nil

	case ActSysctlSet:
		var p SysctlPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		path := r.Plat.Paths().SysctlDir + "/" + p.File
		if p.HadFile {
			old, err := base64.StdEncoding.DecodeString(p.OldFileB64)
			if err != nil {
				return err
			}
			if err := fs.WriteFileAtomic(path, old, 0o644); err != nil {
				return err
			}
		} else if fs.Exists(path) {
			if err := fs.Remove(path); err != nil {
				return err
			}
		}
		// 恢复运行时旧值(best-effort,mock 下同样生效)
		if p.OldRuntime != "" {
			_, err := r.Plat.Run(ctx, platform.RunOpt{
				Argv: []string{"sysctl", "-w", p.Key + "=" + p.OldRuntime}, Timeout: 30 * time.Second,
			})
			return err
		}
		return nil

	case ActDNSStub:
		var b dnsguard.StubBackup
		if err := json.Unmarshal(e.Payload, &b); err != nil {
			return err
		}
		return r.DNS.RevertStub(ctx, b)

	case ActIptables:
		var p IptablesPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		for _, rule := range p.Rules {
			argv := []string{rule.Binary()}
			if rule.Table != "" {
				argv = append(argv, "-t", rule.Table)
			}
			argv = append(argv, "-D", rule.Chain)
			argv = append(argv, rule.Spec...)
			// 规则可能本来就没加上,删除失败不视为错误
			if res, err := r.Plat.Run(ctx, platform.RunOpt{Argv: argv, Timeout: 30 * time.Second}); err != nil {
				r.Log("%s -D 执行异常: %v", rule.Binary(), err)
			} else if res.ExitCode != 0 {
				r.Log("%s -D 未删除(可能规则不存在): %s", rule.Binary(), rule.Chain)
			}
		}
		if res, err := r.Plat.Run(ctx, platform.RunOpt{
			Argv: []string{"netfilter-persistent", "save"}, Timeout: time.Minute,
		}); err != nil || res.ExitCode != 0 {
			r.Log("netfilter-persistent save 未成功(不影响运行时规则)")
		}
		return nil

	case ActServiceState:
		var p SvcPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if !p.WasActive {
			if err := r.Plat.ServiceCtl(ctx, p.Unit, platform.ActStop); err != nil {
				return err
			}
		}
		if !p.WasEnabled {
			return r.Plat.ServiceCtl(ctx, p.Unit, platform.ActDisable)
		}
		return nil

	default:
		return fmt.Errorf("未知 journal 动作: %s", e.Action)
	}
}
