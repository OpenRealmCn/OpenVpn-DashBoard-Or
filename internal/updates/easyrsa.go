package updates

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openvpntools/internal/download"
	"openvpntools/internal/platform"
)

// UpgradeEasyRSA 升级 EasyRSA 到 GitHub 最新 release:
// 强制使用 GitHub API 的 sha256 digest 校验(无 digest 拒绝升级)、
// bash -n 语法检查、迁移 pki 目录、原子换目录、失败恢复旧版本。
func (u *Manager) UpgradeEasyRSA(ctx context.Context, logf func(string, ...any)) error {
	fs := u.plat.FS()
	dir := u.plat.Paths().EasyRSADir
	if !fs.Exists(filepath.Join(dir, "easyrsa")) {
		return errors.New("未找到 EasyRSA(尚未安装 OpenVPN?)")
	}

	u.mu.Lock()
	rel := u.latest
	u.mu.Unlock()
	if rel == nil {
		var err error
		if rel, err = u.fetchLatest(ctx); err != nil {
			return err
		}
	}
	latestVer := strings.TrimPrefix(rel.TagName, "v")
	current := u.readMarker()
	if latestVer == "" || latestVer == current {
		return fmt.Errorf("当前已是最新版本(%s)", current)
	}

	if u.simulate {
		logf("(mock) 升级 EasyRSA %s → %s", current, latestVer)
		return fs.WriteFileAtomic(u.markerPath(), []byte(latestVer), 0o644)
	}

	// 找 tgz 资产与官方 digest
	var asset *releaseAsset
	for i := range rel.Assets {
		name := rel.Assets[i].Name
		if strings.HasPrefix(name, "EasyRSA-") && strings.HasSuffix(name, ".tgz") {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		return errors.New("最新 release 中找不到 EasyRSA-*.tgz 资产")
	}
	sha, ok := strings.CutPrefix(asset.Digest, "sha256:")
	if !ok || len(sha) != 64 {
		return errors.New("GitHub 未提供该资产的 SHA256 digest,为安全起见拒绝自动升级")
	}

	logf("下载 EasyRSA %s(digest 来自 GitHub API,镜像不可信也安全)…", latestVer)
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

	newDir := dir + ".new"
	oldDir := dir + ".old"
	_ = fs.RemoveAll(newDir)
	_ = fs.RemoveAll(oldDir)
	defer fs.RemoveAll(newDir)

	logf("解包并做 bash -n 语法检查 …")
	if err := download.ExtractTarGz(tmp, newDir); err != nil {
		return fmt.Errorf("解包失败: %w", err)
	}
	script := filepath.Join(newDir, "easyrsa")
	if res, err := u.plat.Run(ctx, platform.RunOpt{Argv: []string{"bash", "-n", script}, Timeout: time.Minute}); err != nil || res.ExitCode != 0 {
		return errors.New("新版 easyrsa 脚本语法检查未通过,已中止升级")
	}

	// 迁移 PKI(CA、证书、index.txt 全在里面,绝不能丢)
	logf("迁移 pki 目录到新版本 …")
	if err := fs.Rename(filepath.Join(dir, "pki"), filepath.Join(newDir, "pki")); err != nil {
		return fmt.Errorf("迁移 pki 失败,升级已中止(原目录未动): %w", err)
	}

	// 换目录:旧→.old,新→正式;任一步失败尽力恢复
	if err := fs.Rename(dir, oldDir); err != nil {
		_ = fs.Rename(filepath.Join(newDir, "pki"), filepath.Join(dir, "pki"))
		return fmt.Errorf("移开旧目录失败,已恢复 pki: %w", err)
	}
	if err := fs.Rename(newDir, dir); err != nil {
		_ = fs.Rename(oldDir, dir) // pki 已在 newDir 内,恢复旧目录后再搬回
		_ = fs.Rename(filepath.Join(newDir, "pki"), filepath.Join(dir, "pki"))
		return fmt.Errorf("启用新目录失败,已回退旧版本: %w", err)
	}
	if err := fs.WriteFileAtomic(u.markerPath(), []byte(latestVer), 0o644); err != nil {
		logf("写版本标记失败(不影响使用): %v", err)
	}
	_ = fs.RemoveAll(oldDir)
	logf("EasyRSA 已升级到 %s", latestVer)
	return nil
}
