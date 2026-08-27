package platform

import (
	"os"

	"openvpntools/internal/store"
)

// OSFS 直接落到真实文件系统;linux 实现与 mock 共用
// (mock 的 Paths 全部指向数据目录下的 mockroot,不会触碰系统路径)。
type OSFS struct{}

func (OSFS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (OSFS) WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return store.AtomicWriteFile(path, data, perm)
}

func (OSFS) Remove(path string) error                     { return os.Remove(path) }
func (OSFS) RemoveAll(path string) error                  { return os.RemoveAll(path) }
func (OSFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (OSFS) Rename(oldPath, newPath string) error         { return os.Rename(oldPath, newPath) }
func (OSFS) Symlink(target, link string) error            { return os.Symlink(target, link) }
func (OSFS) Readlink(link string) (string, error)         { return os.Readlink(link) }

func (OSFS) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
