//go:build !linux

package installer

// dpkgLockHolder 非 Linux(mock 开发环境)恒返回未持有。
func dpkgLockHolder() (pid int, comm string, held bool) { return 0, "", false }
