package installer

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"openvpntools/internal/dnsguard"
	"openvpntools/internal/platform"
)

// SharedData 在步骤间传递探测结果。
type SharedData struct {
	OpenVPNVer string   // 如 "2.6"
	OutNIC     string   // 出网网卡
	PushDNS    []string // 最终推送给客户端的 DNS
}

type StepCtx struct {
	Ctx      context.Context
	Plat     platform.Platform
	DNS      *dnsguard.Guard
	Journal  *Journal
	Params   Params
	Data     *SharedData
	Mirror   string // GitHub 镜像前缀
	Simulate bool   // mock 平台上演示运行
	StepID   string // 引擎在执行每步前设置
	Log      func(format string, args ...any)
}

// Step 的撤销逻辑集中在 Rollbacker(按 journal 动作类型),
// 因此步骤只需在动作前 Record 标准动作条目。
type Step struct {
	ID   string
	Name string
	Skip func(c *StepCtx) bool
	Run  func(c *StepCtx) error
}

func buildSteps() []Step {
	return []Step{
		stepPrecheck(),
		stepPackages(),
		stepEasyRSA(),
		stepPKI(),
		stepServerConf(),
		stepSysctl(),
		stepFirewall(),
		stepFreePort53(),
		stepService(),
		stepDNSSelf(),
		stepVerify(),
	}
}

// —— 通用小工具 ——

func tailStr(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// runLogged 执行命令,失败(无法启动或退出码非 0)时返回带输出尾部的错误。
func runLogged(c *StepCtx, env []string, timeout time.Duration, argv ...string) (platform.RunResult, error) {
	res, err := c.Plat.Run(c.Ctx, platform.RunOpt{Argv: argv, Env: env, Timeout: timeout})
	if err != nil {
		return res, fmt.Errorf("%s: %w", argv[0], err)
	}
	if res.ExitCode != 0 {
		out := tailStr(res.Stderr, 500)
		if out == "" {
			out = tailStr(res.Stdout, 500)
		}
		return res, fmt.Errorf("%s 退出码 %d: %s", strings.Join(argv, " "), res.ExitCode, out)
	}
	return res, nil
}

// writeFileJournaled 先记 journal(新建或替换、含旧内容)再写文件。
func writeFileJournaled(c *StepCtx, path string, data []byte, perm os.FileMode) error {
	fs := c.Plat.FS()
	if fs.Exists(path) {
		old, err := fs.ReadFile(path)
		if err != nil {
			return err
		}
		err = c.Journal.Record(c.StepID, ActFileReplaced, FileReplacedPayload{
			Path: path, OldB64: base64.StdEncoding.EncodeToString(old), Perm: uint32(perm),
		})
		if err != nil {
			return err
		}
	} else {
		if err := c.Journal.Record(c.StepID, ActFileCreated, FilePayload{Path: path}); err != nil {
			return err
		}
	}
	return fs.WriteFileAtomic(path, data, perm)
}

func versionModern(ver string) bool {
	var maj, min int
	if _, err := fmt.Sscanf(ver, "%d.%d", &maj, &min); err != nil {
		return true // 解析不出按现代版本处理
	}
	return maj > 2 || (maj == 2 && min >= 5)
}
