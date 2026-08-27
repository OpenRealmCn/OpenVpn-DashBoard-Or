package updates

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"openvpntools/internal/download"
	"openvpntools/internal/version"
)

// SelfUpdate 从 GitHub Release 下载对应架构的新面板二进制并原子替换自身;
// 强制 GitHub API 的 sha256 digest 校验;替换后需重启面板进程生效。
func (u *Manager) SelfUpdate(ctx context.Context, logf func(string, ...any)) error {
	if u.simulate {
		logf("(mock) 面板二进制已替换,重启后生效")
		return nil
	}
	if runtime.GOOS != "linux" {
		return errors.New("面板自更新仅支持 Linux 部署环境")
	}

	u.mu.Lock()
	rel := u.latestPanel
	u.mu.Unlock()
	if rel == nil {
		var err error
		if rel, err = u.fetchPanelLatest(ctx); err != nil {
			return err
		}
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	if !semverGreater(latest, version.Panel) {
		return fmt.Errorf("当前已是最新版本(v%s)", version.Panel)
	}

	want := "ovpn-web-linux-" + runtime.GOARCH
	var asset *releaseAsset
	for i := range rel.Assets {
		if rel.Assets[i].Name == want {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		return fmt.Errorf("release v%s 中找不到本机架构资产 %s", latest, want)
	}
	sha, ok := strings.CutPrefix(asset.Digest, "sha256:")
	if !ok || len(sha) != 64 {
		return errors.New("GitHub 未提供该资产的 SHA256 digest,为安全起见拒绝自动更新")
	}

	logf("下载面板 v%s(%s)…", latest, want)
	tmp, err := download.Fetch(ctx, download.Options{
		URL:    asset.URL,
		Mirror: u.cfg.Snapshot().GithubMirror,
		SHA256: sha,
		Log:    logf,
	})
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	// 临时文件可能与目标不在同一文件系统,先复制到同目录再原子换名
	newPath := exe + ".new"
	if err := copyFile(tmp, newPath, 0o755); err != nil {
		return fmt.Errorf("写入新二进制失败: %w", err)
	}
	oldPath := exe + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(exe, oldPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("移开当前二进制失败: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(oldPath, exe) // 回退
		return fmt.Errorf("启用新二进制失败,已回退: %w", err)
	}
	logf("面板已更新到 v%s(旧版本保留为 %s),重启面板进程后生效", latest, filepath.Base(oldPath))
	return nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// semverGreater 比较 a > b(x.y.z,忽略预发布后缀)。
func semverGreater(a, b string) bool {
	pa, pb := parseVer(a), parseVer(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseVer(s string) [3]int {
	var out [3]int
	s = strings.SplitN(strings.TrimPrefix(s, "v"), "-", 2)[0]
	for i, part := range strings.SplitN(s, ".", 3) {
		if i > 2 {
			break
		}
		n, _ := strconv.Atoi(strings.TrimSpace(part))
		out[i] = n
	}
	return out
}
