package api

import (
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/config"
)

type settingsPayload struct {
	Listen       string `json:"listen"`
	PanelURL     string `json:"panelUrl"`
	GithubMirror string `json:"githubMirror"`
	TLSMode      string `json:"tlsMode"` // off / self / le
	TLSDomain    string `json:"tlsDomain"`
	TLSEmail     string `json:"tlsEmail"`
}

var domainRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

func (s *Server) handleGetSettings(c *gin.Context) {
	snap := s.cfg.Snapshot()
	c.JSON(http.StatusOK, settingsPayload{
		Listen:       snap.Listen,
		PanelURL:     snap.PanelURL,
		GithubMirror: snap.GithubMirror,
		TLSMode:      snap.TLS.EffectiveMode(),
		TLSDomain:    snap.TLS.Domain,
		TLSEmail:     snap.TLS.Email,
	})
}

func (s *Server) handlePutSettings(c *gin.Context) {
	var req settingsPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if req.Listen != "" {
		if _, _, err := net.SplitHostPort(req.Listen); err != nil {
			abortErr(c, http.StatusBadRequest, "监听地址格式应为 host:port")
			return
		}
	}
	for _, u := range []string{req.PanelURL, req.GithubMirror} {
		if u == "" {
			continue
		}
		parsed, err := url.Parse(u)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			abortErr(c, http.StatusBadRequest, "地址必须是完整的 http(s):// URL: "+u)
			return
		}
	}
	switch req.TLSMode {
	case "off", "self":
	case "le":
		if !domainRe.MatchString(req.TLSDomain) {
			abortErr(c, http.StatusBadRequest, "Let's Encrypt 模式需要合法域名(且已解析到本机)")
			return
		}
	default:
		abortErr(c, http.StatusBadRequest, "TLS 模式必须是 off / self / le")
		return
	}
	err := s.cfg.Update(func(cfg *config.Config) error {
		if req.Listen != "" {
			cfg.Listen = req.Listen
		}
		cfg.PanelURL = strings.TrimRight(req.PanelURL, "/")
		cfg.GithubMirror = req.GithubMirror
		cfg.TLS.Mode = req.TLSMode
		cfg.TLS.Enabled = req.TLSMode != "off" // 兼容旧字段
		cfg.TLS.Domain = strings.TrimSpace(req.TLSDomain)
		cfg.TLS.Email = strings.TrimSpace(req.TLSEmail)
		return nil
	})
	if err != nil {
		abortErr(c, http.StatusInternalServerError, "保存设置失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":   true,
		"note": "监听地址与 HTTPS 变更需重启面板后生效;Let's Encrypt 模式将强制监听 443 并占用 80 端口",
	})
}
