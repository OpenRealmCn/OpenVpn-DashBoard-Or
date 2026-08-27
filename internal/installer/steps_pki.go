package installer

import (
	"fmt"
	"path/filepath"
	"time"
)

func stepPKI() Step {
	return Step{ID: "pki", Name: "初始化 PKI 与服务器证书", Run: func(c *StepCtx) error {
		fs := c.Plat.FS()
		paths := c.Plat.Paths()
		pki := paths.PKIDir

		if c.Simulate {
			c.Log("(mock) 生成占位 PKI 文件")
			files := map[string]string{
				filepath.Join(pki, "ca.crt"):                       "MOCK-CA",
				filepath.Join(pki, "issued", "server.crt"):         "MOCK-SERVER-CERT",
				filepath.Join(pki, "private", "server.key"):        "MOCK-SERVER-KEY",
				filepath.Join(pki, "ta.key"):                       "MOCK-TLS-CRYPT",
				filepath.Join(pki, "crl.pem"):                      "MOCK-CRL",
				filepath.Join(pki, "index.txt"):                    "",
			}
			for p, content := range files {
				if err := fs.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return err
				}
				if err := fs.WriteFileAtomic(p, []byte(content), 0o600); err != nil {
					return err
				}
			}
			return nil
		}

		script := filepath.Join(paths.EasyRSADir, "easyrsa")
		env := []string{
			"EASYRSA_BATCH=1",
			"EASYRSA_PKI=" + pki,
			"EASYRSA_ALGO=ec",
			"EASYRSA_CURVE=prime256v1",
			"EASYRSA_CRL_DAYS=3650",
		}
		er := func(args ...string) error {
			argv := append([]string{"bash", script}, args...)
			c.Log("easyrsa %v …", args)
			_, err := runLogged(c, env, 5*time.Minute, argv...)
			return err
		}

		// PKI 位于 EasyRSA 目录内,回滚由 easyrsa 步骤的整目录删除覆盖
		if err := er("init-pki"); err != nil {
			return err
		}
		if err := er("build-ca", "nopass"); err != nil {
			return err
		}
		if err := er("build-server-full", "server", "nopass"); err != nil {
			return err
		}
		if err := er("gen-crl"); err != nil {
			return err
		}

		// tls-crypt 密钥:2.5+ 与 2.4 的 genkey 语法不同
		taKey := filepath.Join(pki, "ta.key")
		var argv []string
		if versionModern(c.Data.OpenVPNVer) {
			argv = []string{"openvpn", "--genkey", "secret", taKey}
		} else {
			argv = []string{"openvpn", "--genkey", "--secret", taKey}
		}
		c.Log("生成 tls-crypt 密钥 …")
		if _, err := runLogged(c, nil, time.Minute, argv...); err != nil {
			return fmt.Errorf("生成 tls-crypt 密钥失败: %w", err)
		}
		c.Log("PKI 初始化完成(EC prime256v1,CRL 有效期 3650 天)")
		return nil
	}}
}
