// Package download 提供带安全约束的 GitHub 下载器:
// TLS ≥ 1.2 且校验证书、指数退避重试、SHA256 内置校验(镜像不可信也安全)、
// tar.gz 解包防路径穿越、bash -n 语法检查。
package download

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EasyRSA 固定版本与官方包校验和(可被面板设置覆盖镜像,但校验和不变)。
const (
	EasyRSAVersion = "3.2.6"
	EasyRSAURL     = "https://github.com/OpenVPN/easy-rsa/releases/download/v3.2.6/EasyRSA-3.2.6.tgz"
	EasyRSASHA256  = "c2572990ce91112eef8d1b8e4a3b58790da95b68501785c621f69121dfbd22d7"
)

type Options struct {
	URL     string
	Mirror  string // 镜像前缀,如 https://ghproxy.example;拼接方式为 mirror + "/" + 原始URL
	SHA256  string // 期望的十六进制摘要,必填
	Retries int    // 总尝试次数,默认 5
	Timeout time.Duration
	Log     func(format string, args ...any)
}

// NewClient 返回统一安全约束的 HTTP 客户端(TLS ≥ 1.2、证书校验、代理环境变量)。
func NewClient(timeout time.Duration) *http.Client { return newClient(timeout) }

func newClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 15 * time.Second,
		},
	}
}

// Fetch 下载到临时文件并完成 SHA256 校验,返回临时文件路径(调用方负责清理)。
func Fetch(ctx context.Context, opt Options) (string, error) {
	if opt.SHA256 == "" {
		return "", errors.New("缺少 SHA256 校验和,拒绝下载")
	}
	logf := opt.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	url := opt.URL
	if opt.Mirror != "" {
		url = strings.TrimRight(opt.Mirror, "/") + "/" + opt.URL
		logf("使用镜像地址下载: %s", url)
	}
	retries := opt.Retries
	if retries <= 0 {
		retries = 5
	}
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	client := newClient(timeout)

	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if attempt > 1 {
			// 指数退避 + 抖动:1s、2s、4s、8s…上限 15s
			backoff := time.Duration(1<<uint(attempt-2)) * time.Second
			if backoff > 15*time.Second {
				backoff = 15 * time.Second
			}
			backoff += time.Duration(rand.Int63n(int64(500 * time.Millisecond)))
			logf("第 %d/%d 次重试,等待 %s …", attempt, retries, backoff.Round(time.Millisecond))
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
		}
		path, err := fetchOnce(ctx, client, url, opt.SHA256)
		if err == nil {
			logf("下载完成,SHA256 校验通过")
			return path, nil
		}
		lastErr = err
		logf("下载失败: %v", err)
		// 4xx(除 429)基本不可能靠重试解决,直接失败
		var he *httpError
		if errors.As(err, &he) && he.code >= 400 && he.code < 500 && he.code != 429 {
			break
		}
	}
	return "", fmt.Errorf("下载失败(已重试): %w", lastErr)
}

type httpError struct{ code int }

func (e *httpError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

func fetchOnce(ctx context.Context, client *http.Client, url, wantSHA string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &httpError{code: resp.StatusCode}
	}

	tmp, err := os.CreateTemp("", "ovpn-dl-*.tgz")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(tmp, h), resp.Body)
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmpName)
		return "", errors.Join(copyErr, closeErr)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantSHA) {
		os.Remove(tmpName)
		return "", fmt.Errorf("SHA256 不匹配: 期望 %s,实际 %s", wantSHA, got)
	}
	return tmpName, nil
}

// ExtractTarGz 把 tgz 解包到 destDir,并剥掉顶层目录(EasyRSA-x.y.z/)。
// 拒绝绝对路径与 .. 穿越;符号链接一律跳过。
func ExtractTarGz(src, destDir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(hdr.Name)
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return fmt.Errorf("压缩包内出现可疑路径,已中止: %q", hdr.Name)
		}
		// 剥掉顶层目录
		parts := strings.SplitN(name, "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		rel := filepath.FromSlash(parts[1])
		target := filepath.Join(destDir, rel)
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("解包目标越界,已中止: %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			// 符号链接/设备文件等一律跳过
		}
	}
}
