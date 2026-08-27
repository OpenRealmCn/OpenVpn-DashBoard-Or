package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/clients"
)

func clientErrCode(err error) int {
	switch {
	case errors.Is(err, clients.ErrNotInstalled):
		return http.StatusConflict
	case errors.Is(err, clients.ErrBadName), errors.Is(err, clients.ErrNotValid):
		return http.StatusBadRequest
	case errors.Is(err, clients.ErrExists):
		return http.StatusConflict
	case errors.Is(err, clients.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) handleClientList(c *gin.Context) {
	list, err := s.clients.List(c.Request.Context())
	if err != nil {
		abortErr(c, clientErrCode(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"clients": list})
}

type createClientReq struct {
	Name       string `json:"name"`
	Passphrase string `json:"passphrase"` // 序列化仅用于请求体,绝不写日志
	ExpireDays int    `json:"expireDays"` // 0 = 默认 825 天
}

func (s *Server) handleClientCreate(c *gin.Context) {
	var req createClientReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if req.Passphrase != "" && len(req.Passphrase) < 4 {
		abortErr(c, http.StatusBadRequest, "证书密码至少 4 位")
		return
	}
	ident := identity(c)
	// 子用户证书配额:统计名下有效证书数
	if !ident.IsAdmin && ident.CertLimit > 0 {
		used, err := s.clients.CountOwnedValid(c.Request.Context(), ident.Username)
		if err == nil && used >= ident.CertLimit {
			abortErr(c, http.StatusForbidden,
				sprintf("已达证书数量上限(%d/%d),吊销闲置证书后可继续创建", used, ident.CertLimit))
			return
		}
	}
	if err := s.clients.Create(c.Request.Context(), req.Name, req.Passphrase, ident.Username, req.ExpireDays); err != nil {
		abortErr(c, clientErrCode(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "encrypted": req.Passphrase != ""})
}

// requireOwnCert 子用户只能操作自己创建的证书;管理员不受限。
func (s *Server) requireOwnCert(c *gin.Context, cn string) bool {
	ident := identity(c)
	if ident.IsAdmin {
		return true
	}
	owner, err := s.clients.OwnerOf(c.Request.Context(), cn)
	if err != nil {
		abortErr(c, clientErrCode(err), err.Error())
		return false
	}
	if owner != ident.Username {
		abortErr(c, http.StatusForbidden, "只能操作自己创建的证书")
		return false
	}
	return true
}

func (s *Server) handleClientRevoke(c *gin.Context) {
	cn := c.Param("cn")
	if !s.requireOwnCert(c, cn) {
		return
	}
	if err := s.clients.Revoke(c.Request.Context(), cn); err != nil {
		abortErr(c, clientErrCode(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) serveProfile(c *gin.Context, cn string) {
	data, filename, err := s.clients.Profile(c.Request.Context(), cn)
	if err != nil {
		abortErr(c, clientErrCode(err), err.Error())
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Data(http.StatusOK, "application/x-openvpn-profile", data)
}

func (s *Server) handleClientProfile(c *gin.Context) {
	cn := c.Param("cn")
	if !s.requireOwnCert(c, cn) {
		return
	}
	s.serveProfile(c, cn)
}

// handleClientShare 生成一次性下载 token,二维码内容即返回的 url。
func (s *Server) handleClientShare(c *gin.Context) {
	cn := c.Param("cn")
	if !s.requireOwnCert(c, cn) {
		return
	}
	// 先确认证书存在且有效,避免发无效 token
	if _, _, err := s.clients.Profile(c.Request.Context(), cn); err != nil {
		abortErr(c, clientErrCode(err), err.Error())
		return
	}
	token, expires := s.qr.Create(cn)
	base := s.cfg.Snapshot().PanelURL
	if base == "" {
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + c.Request.Host
	}
	c.JSON(http.StatusOK, gin.H{
		"url":       base + "/d/" + token,
		"expiresAt": expires.Format(time.RFC3339),
		"ttlSec":    int(time.Until(expires).Seconds()),
	})
}

// handlePublicDownload 扫码免登录一次性下载;token 单次有效,过期/已用返回 410。
func (s *Server) handlePublicDownload(c *gin.Context) {
	cn, ok := s.qr.Consume(c.Param("token"))
	if !ok {
		c.String(http.StatusGone, "链接已失效(一次性下载,过期或已被使用),请在面板重新生成")
		return
	}
	s.serveProfile(c, cn)
}
