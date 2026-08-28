package api

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/audit"
	"openvpntools/internal/auth"
	"openvpntools/internal/users"
)

const identKey = "ovpn-identity"

// requireAuth 校验 Cookie(或主节点的 Bearer node_token)并把身份放入上下文。
func (s *Server) requireAuth(c *gin.Context) {
	// 主节点接管:Authorization: Bearer <node_token>(常量时间比较)
	if ah := c.GetHeader("Authorization"); strings.HasPrefix(ah, "Bearer ") {
		bearer := strings.TrimPrefix(ah, "Bearer ")
		nt := s.cfg.Snapshot().NodeToken
		if nt != "" && len(bearer) == len(nt) &&
			subtle.ConstantTimeCompare([]byte(bearer), []byte(nt)) == 1 {
			c.Set(identKey, auth.MasterIdentity())
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "节点令牌无效"})
		return
	}
	tok, err := c.Cookie(auth.CookieName)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录或会话已过期"})
		return
	}
	ident, err := s.auth.Validate(tok)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录或会话已过期"})
		return
	}
	c.Set(identKey, ident)
	c.Next()
}

// identity 取当前请求身份(必须在 requireAuth 之后调用)。
func identity(c *gin.Context) auth.Identity {
	v, _ := c.Get(identKey)
	ident, _ := v.(auth.Identity)
	return ident
}

// requirePerm 权限门:管理员放行,子用户按权限位判断。
func (s *Server) requirePerm(name string, sel func(users.Perms) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ident := identity(c)
		if !ident.IsAdmin && !sel(ident.Perms) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "没有「" + name + "」权限"})
			return
		}
		c.Next()
	}
}

// requireNodeAccess 节点管理入口:管理员,或被分配了节点的子用户。
func (s *Server) requireNodeAccess(c *gin.Context) {
	ident := identity(c)
	if !ident.IsAdmin && len(ident.NodeIDs) == 0 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "没有可管理的节点"})
		return
	}
	c.Next()
}

func (s *Server) requireAdmin(c *gin.Context) {
	if !identity(c).IsAdmin {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "仅管理员可操作"})
		return
	}
	c.Next()
}

// auditMW 把写操作、一次性下载与异常响应写入持久审计日志(并输出 stdout)。
// 只记录动作与路径,绝不记录请求体(证书密码等敏感字段)。
func (s *Server) auditMW() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		mutating := c.Request.Method != http.MethodGet
		public := strings.HasPrefix(c.Request.URL.Path, "/d/")
		if !mutating && !public && c.Writer.Status() < 400 {
			return
		}
		path := c.Request.URL.Path
		if public {
			path = "/d/<token>" // token 不进日志
		}
		user := "-"
		if ident := identity(c); ident.Username != "" {
			user = ident.Username
		}
		log.Printf("audit %s %s %s -> %d (%s, %s)",
			user, c.Request.Method, path, c.Writer.Status(),
			c.ClientIP(), time.Since(start).Round(time.Millisecond))
		if s.audit != nil && (mutating || public) {
			s.audit.Append(audit.Entry{
				Time: time.Now(), User: user,
				Action: c.Request.Method + " " + path,
				Status: c.Writer.Status(), IP: c.ClientIP(),
			})
		}
	}
}

// rateLimit 按客户端 IP 的固定窗口限流,用于登录与一次性下载入口。
func rateLimit(max int, window time.Duration) gin.HandlerFunc {
	type bucket struct {
		count int
		reset time.Time
	}
	var mu sync.Mutex
	buckets := map[string]*bucket{}

	return func(c *gin.Context) {
		now := time.Now()
		mu.Lock()
		b := buckets[c.ClientIP()]
		if b == nil || now.After(b.reset) {
			b = &bucket{reset: now.Add(window)}
			buckets[c.ClientIP()] = b
		}
		b.count++
		exceeded := b.count > max
		if len(buckets) > 10000 {
			for k, v := range buckets {
				if now.After(v.reset) {
					delete(buckets, k)
				}
			}
		}
		mu.Unlock()
		if exceeded {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "请求过于频繁,请稍后再试"})
			return
		}
		c.Next()
	}
}

func abortErr(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": msg})
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }
