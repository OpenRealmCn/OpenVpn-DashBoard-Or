package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/auth"
	"openvpntools/internal/users"
)

type loginReq struct {
	Username string `json:"username"` // 空 = admin(兼容旧前端)
	Password string `json:"password"`
}

func (s *Server) handleSession(c *gin.Context) {
	resp := gin.H{
		"initialized":   s.auth.Initialized(),
		"authenticated": false,
		"mode":          s.mode,
	}
	if tok, err := c.Cookie(auth.CookieName); err == nil {
		if ident, err := s.auth.Validate(tok); err == nil {
			resp["authenticated"] = true
			used, _ := s.clients.CountOwnedValid(c.Request.Context(), ident.Username)
			resp["user"] = gin.H{
				"username":   ident.Username,
				"isAdmin":    ident.IsAdmin,
				"perms":      ident.Perms,
				"certLimit":  ident.CertLimit,
				"certsUsed":  used,
				"nodeGrants": ident.NodeGrants,
			}
		}
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Server) handleSetup(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if err := s.auth.Setup(req.Password); err != nil {
		switch {
		case errors.Is(err, auth.ErrAlreadySetup):
			abortErr(c, http.StatusConflict, err.Error())
		case errors.Is(err, auth.ErrWeakPassword):
			abortErr(c, http.StatusBadRequest, err.Error())
		default:
			abortErr(c, http.StatusInternalServerError, "保存密码失败: "+err.Error())
		}
		return
	}
	s.issueSession(c, auth.AdminUsername, true)
}

func (s *Server) handleLogin(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	tok, exp, err := s.auth.Login(req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrNotInitialized):
			abortErr(c, http.StatusConflict, err.Error())
		case errors.Is(err, users.ErrDisabled):
			abortErr(c, http.StatusForbidden, err.Error())
		default:
			abortErr(c, http.StatusUnauthorized, "用户名或密码错误")
		}
		return
	}
	s.setSessionCookie(c, tok, exp)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) issueSession(c *gin.Context, username string, admin bool) {
	tok, exp, err := s.auth.IssueToken(username, admin)
	if err != nil {
		abortErr(c, http.StatusInternalServerError, "签发会话失败")
		return
	}
	s.setSessionCookie(c, tok, exp)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) setSessionCookie(c *gin.Context, tok string, exp time.Time) {
	c.SetSameSite(http.SameSiteLaxMode)
	secure := s.cfg.Snapshot().TLS.EffectiveMode() != "off"
	c.SetCookie(auth.CookieName, tok, int(time.Until(exp).Seconds()), "/", "", secure, true)
}

func (s *Server) handleLogout(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(auth.CookieName, "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type changePassReq struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

func (s *Server) handleChangePassword(c *gin.Context) {
	var req changePassReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if err := s.auth.ChangePassword(identity(c), req.OldPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrBadCredential):
			abortErr(c, http.StatusUnauthorized, "旧密码错误")
		case errors.Is(err, auth.ErrWeakPassword), errors.Is(err, users.ErrWeakPass):
			abortErr(c, http.StatusBadRequest, err.Error())
		default:
			abortErr(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
