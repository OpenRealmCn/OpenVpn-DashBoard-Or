package platform

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const defaultRunTimeout = 5 * time.Minute

// AptLockWaitSec 是 apt 等待 dpkg 锁的秒数。新装系统开机后
// unattended-upgrades 等自动任务常持锁数分钟,不等锁会让
// 安装与回滚直接死在 "Could not get lock /var/lib/dpkg/lock-frontend" 上。
const AptLockWaitSec = 300

// AptGet 构造带 dpkg 锁等待的 apt-get 命令行;所有 apt 调用统一经此入口。
func AptGet(args ...string) []string {
	return append([]string{"apt-get", "-o", "DPkg::Lock::Timeout=" + strconv.Itoa(AptLockWaitSec)}, args...)
}

// ExecRun 以 argv 数组直接执行外部命令(绝不经过 shell)。
// 命令跑完但退出码非 0 时不视为 error,由调用方检查 ExitCode;
// error 仅表示命令无法启动或超时被杀。
func ExecRun(ctx context.Context, opt RunOpt) (RunResult, error) {
	if len(opt.Argv) == 0 {
		return RunResult{ExitCode: -1}, errors.New("argv 为空")
	}
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = defaultRunTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, opt.Argv[0], opt.Argv[1:]...)
	cmd.Env = append(os.Environ(), opt.Env...)
	cmd.Stdin = opt.Stdin
	cmd.ExtraFiles = opt.ExtraFiles
	cmd.Dir = opt.Dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	res := RunResult{Stdout: stdout.String(), Stderr: stderr.String()}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.ExitCode = 0
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
		if ctx.Err() != nil {
			return res, context.Cause(ctx)
		}
		return res, nil
	default:
		res.ExitCode = -1
		return res, err
	}
	return res, nil
}
