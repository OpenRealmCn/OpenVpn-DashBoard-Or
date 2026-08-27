package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"openvpntools/internal/download"
	"openvpntools/internal/platform"
)

// RunOptArgv 简写:默认超时的 argv 执行选项。
func RunOptArgv(argv ...string) platform.RunOpt {
	return platform.RunOpt{Argv: argv, Timeout: 2 * time.Minute}
}

func stepEasyRSA() Step {
	return Step{ID: "easyrsa", Name: "下载并部署 EasyRSA", Run: func(c *StepCtx) error {
		fs := c.Plat.FS()
		dir := c.Plat.Paths().EasyRSADir
		if fs.Exists(dir) {
			return fmt.Errorf("EasyRSA 目录已存在(%s),疑似残留,请先清理", dir)
		}
		// write-ahead:回滚时整目录删除
		if err := c.Journal.Record(c.StepID, ActDirCreated, FilePayload{Path: dir}); err != nil {
			return err
		}

		if c.Simulate {
			c.Log("(mock) 跳过真实下载,生成占位 EasyRSA")
			if err := fs.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			if err := fs.WriteFileAtomic(filepath.Join(dir, "easyrsa"),
				[]byte("#!/bin/sh\n# mock easyrsa\n"), 0o755); err != nil {
				return err
			}
			return fs.WriteFileAtomic(filepath.Join(dir, ".openvpntools-version"),
				[]byte(download.EasyRSAVersion), 0o644)
		}

		c.Log("下载 EasyRSA v%s(TLS≥1.2,SHA256 校验)…", download.EasyRSAVersion)
		tmp, err := download.Fetch(c.Ctx, download.Options{
			URL:    download.EasyRSAURL,
			Mirror: c.Mirror,
			SHA256: download.EasyRSASHA256,
			Log:    c.Log,
		})
		if err != nil {
			return err
		}
		defer os.Remove(tmp)

		extractDir := dir + ".extract"
		_ = fs.RemoveAll(extractDir)
		defer fs.RemoveAll(extractDir) // 成功 rename 后为空操作
		c.Log("解包(防路径穿越)…")
		if err := download.ExtractTarGz(tmp, extractDir); err != nil {
			return fmt.Errorf("解包失败: %w", err)
		}

		script := filepath.Join(extractDir, "easyrsa")
		if !fs.Exists(script) {
			return fmt.Errorf("压缩包内缺少 easyrsa 脚本")
		}
		c.Log("bash -n 语法检查 easyrsa 脚本 …")
		if _, err := runLogged(c, nil, time.Minute, "bash", "-n", script); err != nil {
			return fmt.Errorf("easyrsa 脚本语法检查未通过,拒绝执行: %w", err)
		}

		if err := fs.Rename(extractDir, dir); err != nil {
			return err
		}
		// 版本标记供后续"检查更新"使用
		if err := fs.WriteFileAtomic(filepath.Join(dir, ".openvpntools-version"),
			[]byte(download.EasyRSAVersion), 0o644); err != nil {
			c.Log("写版本标记失败(不影响安装): %v", err)
		}
		c.Log("EasyRSA 就绪: %s", dir)
		return nil
	}}
}
