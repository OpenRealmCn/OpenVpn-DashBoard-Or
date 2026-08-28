package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/users"
)

type userDTO struct {
	Username  string      `json:"username"`
	Perms     users.Perms `json:"perms"`
	CertLimit int         `json:"certLimit"`
	CertsUsed int         `json:"certsUsed"`
	NodeIDs   []string    `json:"nodeIds"`
	Disabled  bool        `json:"disabled"`
	CreatedAt string      `json:"createdAt"`
}

func userErrCode(err error) int {
	switch {
	case errors.Is(err, users.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, users.ErrExists):
		return http.StatusConflict
	case errors.Is(err, users.ErrBadName), errors.Is(err, users.ErrReserved),
		errors.Is(err, users.ErrWeakPass):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) handleUserList(c *gin.Context) {
	list := s.users.List()
	out := make([]userDTO, 0, len(list))
	for _, u := range list {
		used, _ := s.clients.CountOwnedValid(c.Request.Context(), u.Username)
		out = append(out, userDTO{
			Username: u.Username, Perms: u.Perms, CertLimit: u.CertLimit,
			CertsUsed: used, NodeIDs: u.NodeIDs, Disabled: u.Disabled,
			CreatedAt: u.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	c.JSON(http.StatusOK, gin.H{"users": out})
}

type userCreateReq struct {
	Username  string      `json:"username"`
	Password  string      `json:"password"`
	Perms     users.Perms `json:"perms"`
	CertLimit int         `json:"certLimit"`
	NodeIDs   []string    `json:"nodeIds"`
}

func (s *Server) handleUserCreate(c *gin.Context) {
	var req userCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if err := s.users.Create(req.Username, req.Password, req.Perms, req.CertLimit, req.NodeIDs); err != nil {
		abortErr(c, userErrCode(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type userUpdateReq struct {
	Perms     *users.Perms `json:"perms"`
	CertLimit *int         `json:"certLimit"`
	NodeIDs   *[]string    `json:"nodeIds"`
	Disabled  *bool        `json:"disabled"`
	Password  string       `json:"password"` // 非空 = 重置密码
}

func (s *Server) handleUserUpdate(c *gin.Context) {
	name := c.Param("name")
	var req userUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	err := s.users.Update(name, func(u *users.User) error {
		if req.Perms != nil {
			u.Perms = *req.Perms
		}
		if req.CertLimit != nil && *req.CertLimit >= 0 {
			u.CertLimit = *req.CertLimit
		}
		if req.NodeIDs != nil {
			u.NodeIDs = *req.NodeIDs
		}
		if req.Disabled != nil {
			u.Disabled = *req.Disabled
		}
		return nil
	})
	if err != nil {
		abortErr(c, userErrCode(err), err.Error())
		return
	}
	if req.Password != "" {
		if err := s.users.SetPassword(name, req.Password); err != nil {
			abortErr(c, userErrCode(err), err.Error())
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) handleUserDelete(c *gin.Context) {
	if err := s.users.Delete(c.Param("name")); err != nil {
		abortErr(c, userErrCode(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) handleAuditTail(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"entries": s.audit.Tail(200)})
}
