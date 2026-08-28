//go:build linux

package installer

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// dpkgLockHolder 用 F_GETLK 探测 dpkg frontend 锁的持有者。
// apt/dpkg 用的是 fcntl 记录锁,flock(1) 探测不到,必须走同类查询;
// F_GETLK 只查询不加锁,对持有者没有任何影响。
func dpkgLockHolder() (pid int, comm string, held bool) {
	f, err := os.OpenFile("/var/lib/dpkg/lock-frontend", os.O_RDWR, 0)
	if err != nil {
		// 文件不存在或无权限(非 root):无法探测,交由 apt 自身的等锁兜底
		return 0, "", false
	}
	defer f.Close()
	lk := syscall.Flock_t{Type: syscall.F_WRLCK}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_GETLK, &lk); err != nil {
		return 0, "", false
	}
	if lk.Type == syscall.F_UNLCK {
		return 0, "", false
	}
	pid = int(lk.Pid)
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
		comm = strings.TrimSpace(string(data))
	}
	return pid, comm, true
}
