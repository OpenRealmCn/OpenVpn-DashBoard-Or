// Package api 提供 REST 路由与 SPA 静态资源托管。
// 路由按权限分组:view / certCreate / certRevoke / install / kick / maintain,
// 用户管理、面板设置与备份仅管理员可用。
package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/audit"
	"openvpntools/internal/auth"
	"openvpntools/internal/clients"
	"openvpntools/internal/config"
	"openvpntools/internal/dnsguard"
	"openvpntools/internal/installer"
	"openvpntools/internal/nodes"
	"openvpntools/internal/platform"
	"openvpntools/internal/qrlink"
	"openvpntools/internal/updates"
	"openvpntools/internal/users"
)

type Server struct {
	cfg     *config.Manager
	auth    *auth.Service
	users   *users.Store
	plat    platform.Platform
	dns     *dnsguard.Guard
	engine    *installer.Engine
	clients   *clients.Manager
	qr        *qrlink.Store
	updates   *updates.Manager
	audit     *audit.Logger
	nodes     *nodes.Store
	joinCodes *nodes.JoinCodes
	mode      string // linux / mock,状态页展示用
}

func New(cfg *config.Manager, authSvc *auth.Service, us *users.Store,
	plat platform.Platform, auditLog *audit.Logger, nodeStore *nodes.Store, mode string) *Server {
	dns := dnsguard.New(plat)
	simulate := mode == "mock"
	return &Server{
		cfg: cfg, auth: authSvc, users: us, plat: plat, dns: dns,
		engine:    installer.NewEngine(plat, dns, cfg, simulate),
		clients:   clients.New(plat, simulate),
		qr:        qrlink.New(qrlink.DefaultTTL),
		updates:   updates.New(plat, cfg, simulate),
		audit:     auditLog,
		nodes:     nodeStore,
		joinCodes: nodes.NewJoinCodes(),
		mode:      mode,
	}
}

func (s *Server) Router(static fs.FS) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), s.auditMW())

	r.GET("/api/session", s.handleSession)
	r.POST("/api/setup", rateLimit(10, time.Minute), s.handleSetup)
	r.POST("/api/login", rateLimit(10, time.Minute), s.handleLogin)

	// 子节点自注册(凭一次性绑定码)
	r.POST("/api/nodes/register", rateLimit(10, time.Minute), s.handleNodeRegister)

	authed := r.Group("/api", s.requireAuth)
	authed.POST("/logout", s.handleLogout)
	authed.POST("/auth/password", s.handleChangePassword)
	// 主节点健康探测(Bearer node_token 即为管理员身份)
	authed.GET("/node/ping", s.handleNodePing)

	view := authed.Group("", s.requirePerm("查看", func(p users.Perms) bool { return p.View }))
	{
		view.GET("/status", s.handleStatus)
		view.GET("/dns/stub", s.handleDNSStub)
		view.GET("/install/state", s.handleInstallState)
		view.GET("/clients", s.handleClientList)
		view.GET("/online", s.handleClientsOnline)
		view.GET("/version", s.handleVersion)
	}

	certC := authed.Group("", s.requirePerm("创建证书", func(p users.Perms) bool { return p.CertCreate }))
	{
		certC.POST("/clients", s.handleClientCreate)
		certC.GET("/clients/:cn/config", s.handleClientProfile)
		certC.POST("/clients/:cn/share", s.handleClientShare)
	}

	certR := authed.Group("", s.requirePerm("吊销证书", func(p users.Perms) bool { return p.CertRevoke }))
	{
		certR.POST("/clients/:cn/revoke", s.handleClientRevoke)
	}

	inst := authed.Group("", s.requirePerm("安装", func(p users.Perms) bool { return p.Install }))
	{
		inst.POST("/install/precheck", s.handleInstallPrecheck)
		inst.POST("/install", s.handleInstallStart)
		inst.POST("/install/rollback", s.handleInstallRollback)
		inst.GET("/install/events", s.handleInstallEvents)
	}

	kick := authed.Group("", s.requirePerm("断开客户端", func(p users.Perms) bool { return p.Kick }))
	{
		kick.POST("/online/:cn/kick", s.handleClientKick)
	}

	maint := authed.Group("", s.requirePerm("系统维护", func(p users.Perms) bool { return p.Maintain }))
	{
		maint.POST("/dns/stub/disable", s.handleDNSStubDisable)
		maint.POST("/dns/stub/restore", s.handleDNSStubRestore)
		maint.POST("/system/ipforward", s.handleFixIPForward)
		maint.POST("/service/openvpn/:action", s.handleServiceCtl)
		maint.POST("/update/openvpn", s.handleUpgradeOpenVPN)
		maint.POST("/update/easyrsa", s.handleUpgradeEasyRSA)
	}

	// 节点管理:管理员全量;子用户仅限被分配的节点(接口内再按节点归属校验)
	nodeGrp := authed.Group("", s.requireNodeAccess)
	{
		nodeGrp.GET("/nodes", s.handleNodeList)
		nodeGrp.POST("/nodes/batch", s.handleNodeBatch)
		nodeGrp.Any("/nodes/:id/proxy/*rest", s.handleNodeProxy)
	}

	adm := authed.Group("", s.requireAdmin)
	{
		adm.GET("/settings", s.handleGetSettings)
		adm.PUT("/settings", s.handlePutSettings)
		adm.GET("/users", s.handleUserList)
		adm.POST("/users", s.handleUserCreate)
		adm.PUT("/users/:name", s.handleUserUpdate)
		adm.DELETE("/users/:name", s.handleUserDelete)
		adm.GET("/audit", s.handleAuditTail)
		adm.GET("/backup", s.handleBackup)
		adm.POST("/backup/restore", s.handleRestore)
		adm.POST("/update/panel", s.handleUpgradePanel)
		adm.POST("/panel/restart", s.handlePanelRestart)
		adm.POST("/nodes", s.handleNodeAdd)
		adm.PUT("/nodes/:id", s.handleNodeUpdate)
		adm.DELETE("/nodes/:id", s.handleNodeDelete)
		adm.POST("/nodes/joincode", s.handleNodeJoinCode)
	}

	// 扫码免登录一次性下载(token 即凭证,全局限流防爆破)
	r.GET("/d/:token", rateLimit(20, time.Minute), s.handlePublicDownload)

	registerSPA(r, static)
	return r
}

// registerSPA 托管内嵌前端;未知路径回退 index.html(前端路由接管)。
// static 为 nil 时(-tags dev 构建)只提供 API,前端由 Vite dev server 代理。
func registerSPA(r *gin.Engine, static fs.FS) {
	if static == nil {
		r.NoRoute(func(c *gin.Context) {
			if isAPIPath(c.Request.URL.Path) {
				c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
				return
			}
			c.String(http.StatusOK, "dev 构建:请在 web/ 目录运行 npm run dev,由 Vite 提供前端页面")
		})
		return
	}
	fileServer := http.FileServer(http.FS(static))
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if isAPIPath(p) {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		clean := strings.TrimPrefix(path.Clean(p), "/")
		if clean != "" {
			if f, err := static.Open(clean); err == nil {
				f.Close()
			} else {
				c.Request.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}

func isAPIPath(p string) bool {
	return strings.HasPrefix(p, "/api/") || p == "/api" || strings.HasPrefix(p, "/d/")
}
