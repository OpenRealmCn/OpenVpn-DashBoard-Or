package openvpn

import (
	"fmt"
	"strings"
)

type ClientConfParams struct {
	Remote     string // 公网 IP 或域名
	Port       int
	Proto      string
	CA         string // PEM
	Cert       string // PEM
	Key        string // PEM
	TLSCrypt   string // ta.key 内容
}

// RenderClientConf 生成内联证书的 .ovpn(单文件即可导入)。
func RenderClientConf(p ClientConfParams) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w("client")
	w("dev tun")
	w("proto %s", p.Proto)
	w("remote %s %d", p.Remote, p.Port)
	w("resolv-retry infinite")
	w("nobind")
	w("persist-key")
	w("persist-tun")
	w("remote-cert-tls server")
	w("auth SHA256")
	w("cipher AES-256-GCM")
	// 旧客户端(2.4)不认识 data-ciphers,先声明忽略避免报错
	w("ignore-unknown-option data-ciphers")
	w("data-ciphers AES-256-GCM:AES-128-GCM")
	w("tls-version-min 1.2")
	w("verb 3")
	w("<ca>\n%s</ca>", ensureNL(p.CA))
	w("<cert>\n%s</cert>", ensureNL(p.Cert))
	w("<key>\n%s</key>", ensureNL(p.Key))
	w("<tls-crypt>\n%s</tls-crypt>", ensureNL(p.TLSCrypt))
	return b.String()
}

func ensureNL(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return s
	}
	return s + "\n"
}

// ExtractPEM 取出文本中第一个 PEM 块(easyrsa 签发的 .crt 前面带说明文字)。
func ExtractPEM(content string) string {
	start := strings.Index(content, "-----BEGIN")
	if start < 0 {
		return strings.TrimSpace(content)
	}
	endMark := "-----END"
	end := strings.Index(content[start:], endMark)
	if end < 0 {
		return strings.TrimSpace(content[start:])
	}
	rest := content[start+end:]
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		return strings.TrimSpace(content[start:])
	}
	return strings.TrimSpace(content[start : start+end+nl])
}
