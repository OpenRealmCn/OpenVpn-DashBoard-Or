// Package easyrsa 封装 EasyRSA 命令与 pki/index.txt 解析。
// 客户端证书密码经管道 fd 传入(EASYRSA_PASSOUT=file:/dev/fd/3),
// 不出现在 argv、环境变量值、磁盘与日志中。
package easyrsa

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openvpntools/internal/platform"
)

type EasyRSA struct {
	plat platform.Platform
}

func New(plat platform.Platform) *EasyRSA { return &EasyRSA{plat: plat} }

func (e *EasyRSA) script() string {
	return filepath.Join(e.plat.Paths().EasyRSADir, "easyrsa")
}

func (e *EasyRSA) env() []string {
	return []string{
		"EASYRSA_BATCH=1",
		"EASYRSA_PKI=" + e.plat.Paths().PKIDir,
		"EASYRSA_ALGO=ec",
		"EASYRSA_CURVE=prime256v1",
		"EASYRSA_CRL_DAYS=3650",
	}
}

func (e *EasyRSA) run(ctx context.Context, extraEnv []string, extraFiles []*os.File, args ...string) error {
	argv := append([]string{"bash", e.script()}, args...)
	res, err := e.plat.Run(ctx, platform.RunOpt{
		Argv:       argv,
		Env:        append(e.env(), extraEnv...),
		ExtraFiles: extraFiles,
		Timeout:    5 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("easyrsa %s: %w", args[0], err)
	}
	if res.ExitCode != 0 {
		out := strings.TrimSpace(res.Stderr)
		if out == "" {
			out = strings.TrimSpace(res.Stdout)
		}
		if len(out) > 500 {
			out = "…" + out[len(out)-500:]
		}
		return fmt.Errorf("easyrsa %s 失败(退出码 %d): %s", args[0], res.ExitCode, out)
	}
	return nil
}

// BuildClientFull 创建客户端证书;passphrase 为空则 nopass,
// 否则通过管道 fd3 传给 openssl 加密私钥;expireDays > 0 时自定义有效期。
func (e *EasyRSA) BuildClientFull(ctx context.Context, cn, passphrase string, expireDays int) error {
	var extraEnv []string
	if expireDays > 0 {
		extraEnv = append(extraEnv, fmt.Sprintf("EASYRSA_CERT_EXPIRE=%d", expireDays))
	}
	if passphrase == "" {
		return e.run(ctx, extraEnv, nil, "build-client-full", cn, "nopass")
	}
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer r.Close()
	go func() {
		// 写入后立即关闭写端,openssl 读到 EOF 前拿到整行密码
		_, _ = w.WriteString(passphrase + "\n")
		_ = w.Close()
	}()
	// ExtraFiles 的第一个文件映射为子进程 fd 3
	return e.run(ctx,
		append(extraEnv, "EASYRSA_PASSOUT=file:/dev/fd/3"),
		[]*os.File{r},
		"build-client-full", cn)
}

func (e *EasyRSA) Revoke(ctx context.Context, cn string) error {
	if err := e.run(ctx, nil, nil, "revoke", cn); err != nil {
		return err
	}
	return e.GenCRL(ctx)
}

func (e *EasyRSA) GenCRL(ctx context.Context) error {
	return e.run(ctx, nil, nil, "gen-crl")
}
