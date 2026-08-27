// Package backup 实现备份包(tar.gz)的安全恢复:
// 防路径穿越、白名单条目、被替换的现状先移入 restore-backup-* 目录留底。
package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openvpntools/internal/platform"
)

type Summary struct {
	PkiFiles    int    `json:"pkiFiles"`
	ServerFiles int    `json:"serverFiles"`
	DataFiles   int    `json:"dataFiles"`
	BackupDir   string `json:"backupDir"` // 被替换的原文件留底位置
}

var dataWhitelist = map[string]bool{
	"install.json": true, "users.json": true, "cert-owners.json": true,
}

// Restore 从备份流恢复 PKI、server 配置与面板数据。
func Restore(plat platform.Platform, r io.Reader) (Summary, error) {
	var sum Summary
	paths := plat.Paths()
	staging := filepath.Join(paths.DataDir, "restore-staging")
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return sum, err
	}
	defer os.RemoveAll(staging)

	gz, err := gzip.NewReader(r)
	if err != nil {
		return sum, errors.New("不是合法的 tar.gz 备份文件")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return sum, fmt.Errorf("读取备份包失败: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.ToSlash(hdr.Name)
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return sum, fmt.Errorf("备份包内出现可疑路径,已中止: %q", hdr.Name)
		}
		var dest string
		switch {
		case strings.HasPrefix(name, "pki/"):
			dest = filepath.Join(staging, "pki", filepath.FromSlash(strings.TrimPrefix(name, "pki/")))
			sum.PkiFiles++
		case strings.HasPrefix(name, "openvpn-server/"):
			dest = filepath.Join(staging, "server", filepath.FromSlash(strings.TrimPrefix(name, "openvpn-server/")))
			sum.ServerFiles++
		case strings.HasPrefix(name, "data/"):
			base := strings.TrimPrefix(name, "data/")
			if !dataWhitelist[base] {
				continue
			}
			dest = filepath.Join(staging, "data", base)
			sum.DataFiles++
		default:
			continue
		}
		if !strings.HasPrefix(dest, filepath.Clean(staging)+string(os.PathSeparator)) {
			return sum, fmt.Errorf("解包目标越界,已中止: %q", hdr.Name)
		}
		if err := writeFile(dest, tr, restorePerm(name)); err != nil {
			return sum, err
		}
	}

	if sum.PkiFiles > 0 && !fileExists(filepath.Join(staging, "pki", "ca.crt")) {
		return sum, errors.New("备份包的 pki 缺少 ca.crt,拒绝恢复")
	}
	if sum.PkiFiles+sum.ServerFiles+sum.DataFiles == 0 {
		return sum, errors.New("备份包里没有可恢复的内容")
	}

	// 留底目录:被替换的现状全部移入,便于人工回退
	sum.BackupDir = filepath.Join(paths.DataDir, "restore-backup-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(sum.BackupDir, 0o700); err != nil {
		return sum, err
	}

	if sum.PkiFiles > 0 {
		if err := swapDir(filepath.Join(staging, "pki"), paths.PKIDir, filepath.Join(sum.BackupDir, "pki")); err != nil {
			return sum, fmt.Errorf("恢复 pki 失败: %w", err)
		}
	}
	if sum.ServerFiles > 0 {
		if err := restoreFiles(filepath.Join(staging, "server"), paths.ServerConfDir, filepath.Join(sum.BackupDir, "server")); err != nil {
			return sum, fmt.Errorf("恢复 server 配置失败: %w", err)
		}
	}
	if sum.DataFiles > 0 {
		if err := restoreFiles(filepath.Join(staging, "data"), paths.DataDir, filepath.Join(sum.BackupDir, "data")); err != nil {
			return sum, fmt.Errorf("恢复面板数据失败: %w", err)
		}
	}
	return sum, nil
}

func restorePerm(name string) os.FileMode {
	base := filepath.Base(name)
	switch base {
	case "server.conf", "ca.crt", "server.crt", "crl.pem":
		return 0o644
	}
	if strings.HasSuffix(base, ".crt") || strings.HasSuffix(base, ".pem") || base == "index.txt" {
		return 0o644
	}
	return 0o600
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func writeFile(dest string, r io.Reader, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// moveTree 优先 rename,跨设备(EXDEV)时退化为复制后删除。
func moveTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		return writeFile(target, in, info.Mode()&0o777)
	})
}

// swapDir:现有目录整体移入留底,再把 staged 目录放到目标位置。
func swapDir(staged, current, backupTo string) error {
	if fileExists(current) {
		if err := moveTree(current, backupTo); err != nil {
			return err
		}
	}
	return moveTree(staged, current)
}

// restoreFiles 按文件逐个替换:旧文件留底,新文件就位。
func restoreFiles(stagedDir, destDir, backupTo string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	return filepath.Walk(stagedDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(stagedDir, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(destDir, rel)
		if fileExists(dest) {
			if err := moveTree(dest, filepath.Join(backupTo, rel)); err != nil {
				return err
			}
		}
		return moveTree(path, dest)
	})
}
