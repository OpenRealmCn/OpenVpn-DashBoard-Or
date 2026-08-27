package nodes

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Health 是子节点 /api/node/ping 的响应。
type Health struct {
	Reachable     bool   `json:"reachable"`
	Version       string `json:"version"`
	Mode          string `json:"mode"`
	Installed     bool   `json:"installed"`
	ServiceActive bool   `json:"serviceActive"`
	Online        int    `json:"online"`
	Error         string `json:"error,omitempty"`
}

// HTTPClient 与子节点通信的客户端;insecure 仅用于自签 HTTPS 的子节点
//(令牌仍是唯一凭据,用户需自行承担中间人风险,UI 有明示)。
func HTTPClient(insecure bool, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: insecure, //nolint:gosec — 用户显式选择
			},
		},
	}
}

// NewRequest 构造发往子节点的请求(带 Bearer 令牌)。
func NewRequest(ctx context.Context, n Node, method, apiPath string, body io.Reader) (*http.Request, error) {
	target := strings.TrimRight(n.URL, "/") + "/" + strings.TrimLeft(apiPath, "/")
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+n.Token)
	return req, nil
}

// Ping 健康探测(5 秒超时)。
func Ping(ctx context.Context, n Node) Health {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := NewRequest(ctx, n, http.MethodGet, "/api/node/ping", nil)
	if err != nil {
		return Health{Error: err.Error()}
	}
	resp, err := HTTPClient(n.InsecureTLS, 6*time.Second).Do(req)
	if err != nil {
		return Health{Error: "不可达: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Health{Error: fmt.Sprintf("HTTP %d(令牌无效或版本过旧)", resp.StatusCode)}
	}
	var h Health
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&h); err != nil {
		return Health{Error: "响应解析失败"}
	}
	h.Reachable = true
	return h
}
