package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/auth"
	"openvpntools/internal/nodes"
	"openvpntools/internal/openvpn"
	"openvpntools/internal/users"
	"openvpntools/internal/version"
)

// handleNodePing 子节点健康端点:主节点凭 Bearer node_token 调用。
func (s *Server) handleNodePing(c *gin.Context) {
	ctx := c.Request.Context()
	svc, _ := s.plat.ServiceStatus(ctx, openvpn.ServiceUnit)
	online := 0
	if list, err := s.clients.Online(ctx); err == nil {
		online = len(list)
	}
	c.JSON(http.StatusOK, gin.H{
		"version":       version.Panel,
		"mode":          s.mode,
		"installed":     s.engine.Installed(),
		"serviceActive": svc.Active,
		"online":        online,
	})
}

// grantFor 返回用户对节点的授权;管理员返回 (nil, true) 表示全权。
func grantFor(ident auth.Identity, id string) (*users.NodeGrant, bool) {
	if ident.IsAdmin {
		return nil, true
	}
	for i := range ident.NodeGrants {
		if ident.NodeGrants[i].NodeID == id {
			return &ident.NodeGrants[i], true
		}
	}
	return nil, false
}

// permNeeded 把发往子节点的请求映射为所需的节点权限位;
// 未识别的写操作与备份接口仅「完整管理」可执行(默认拒绝)。
func permNeeded(method, rest string) func(g *users.NodeGrant) bool {
	need := func(sel func(users.NodePerms) bool) func(*users.NodeGrant) bool {
		return func(g *users.NodeGrant) bool {
			if g == nil || g.Full {
				return true
			}
			return sel(g.Perms)
		}
	}
	fullOnly := func(g *users.NodeGrant) bool { return g == nil || g.Full }

	rest = strings.TrimPrefix(rest, "/")
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest = rest[:i]
	}
	switch {
	case strings.HasPrefix(rest, "backup"):
		return fullOnly
	case method == http.MethodGet && strings.HasPrefix(rest, "clients/") && strings.HasSuffix(rest, "/config"):
		return need(func(p users.NodePerms) bool { return p.CertCreate })
	case method == http.MethodGet:
		return need(func(p users.NodePerms) bool { return p.View })
	case rest == "clients", strings.HasSuffix(rest, "/share"):
		return need(func(p users.NodePerms) bool { return p.CertCreate })
	case strings.HasSuffix(rest, "/revoke"):
		return need(func(p users.NodePerms) bool { return p.CertRevoke })
	case rest == "install", rest == "install/precheck":
		return need(func(p users.NodePerms) bool { return p.Install })
	case rest == "install/rollback":
		return need(func(p users.NodePerms) bool { return p.Rollback })
	case strings.HasPrefix(rest, "service/"):
		return need(func(p users.NodePerms) bool { return p.Service })
	case strings.HasPrefix(rest, "online/"):
		return need(func(p users.NodePerms) bool { return p.Kick })
	case strings.HasPrefix(rest, "update/"):
		return need(func(p users.NodePerms) bool { return p.Upgrade })
	case rest == "panel/restart":
		return need(func(p users.NodePerms) bool { return p.PanelRestart })
	case strings.HasPrefix(rest, "dns/"), strings.HasPrefix(rest, "system/"):
		return need(func(p users.NodePerms) bool { return p.Upgrade })
	default:
		return fullOnly
	}
}

type nodeDTO struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	URL         string           `json:"url"`
	InsecureTLS bool             `json:"insecureTLS"`
	AddedAt     time.Time        `json:"addedAt"`
	Health      nodes.Health     `json:"health"`
	Grant       *users.NodeGrant `json:"grant,omitempty"` // 当前用户对该节点的授权;空 = 全权(管理员)
}

// handleNodeList 列出节点并并发健康探测(令牌绝不外传);子用户仅见被分配的节点。
func (s *Server) handleNodeList(c *gin.Context) {
	ident := identity(c)
	list := s.nodes.List()
	grants := map[string]*users.NodeGrant{}
	if !ident.IsAdmin {
		filtered := make([]nodes.Node, 0, len(list))
		for _, n := range list {
			if g, ok := grantFor(ident, n.ID); ok {
				filtered = append(filtered, n)
				grants[n.ID] = g
			}
		}
		list = filtered
	}
	out := make([]nodeDTO, len(list))
	var wg sync.WaitGroup
	for i, n := range list {
		out[i] = nodeDTO{
			ID: n.ID, Name: n.Name, URL: n.URL, InsecureTLS: n.InsecureTLS,
			AddedAt: n.AddedAt, Grant: grants[n.ID],
		}
		wg.Add(1)
		go func(i int, n nodes.Node) {
			defer wg.Done()
			out[i].Health = nodes.Ping(c.Request.Context(), n)
		}(i, n)
	}
	wg.Wait()
	c.JSON(http.StatusOK, gin.H{"nodes": out})
}

type nodeAddReq struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Token       string `json:"token"`
	InsecureTLS bool   `json:"insecureTLS"`
}

func (s *Server) handleNodeAdd(c *gin.Context) {
	var req nodeAddReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	u, err := nodes.NormalizeURL(req.URL)
	if err != nil {
		abortErr(c, http.StatusBadRequest, err.Error())
		return
	}
	// 先验证连通与令牌,再入表
	probe := nodes.Ping(c.Request.Context(), nodes.Node{URL: u, Token: req.Token, InsecureTLS: req.InsecureTLS})
	if !probe.Reachable {
		abortErr(c, http.StatusBadGateway, "无法接管子节点: "+probe.Error)
		return
	}
	n, err := s.nodes.Add(req.Name, u, req.Token, req.InsecureTLS)
	if err != nil {
		abortErr(c, http.StatusConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": n.ID})
}

type nodeUpdateReq struct {
	Name        *string `json:"name"`
	URL         *string `json:"url"`
	Token       *string `json:"token"`
	InsecureTLS *bool   `json:"insecureTLS"`
}

func (s *Server) handleNodeUpdate(c *gin.Context) {
	var req nodeUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	err := s.nodes.Update(c.Param("id"), func(n *nodes.Node) error {
		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			n.Name = strings.TrimSpace(*req.Name)
		}
		if req.URL != nil {
			u, err := nodes.NormalizeURL(*req.URL)
			if err != nil {
				return err
			}
			n.URL = u
		}
		if req.Token != nil && *req.Token != "" {
			n.Token = *req.Token
		}
		if req.InsecureTLS != nil {
			n.InsecureTLS = *req.InsecureTLS
		}
		return nil
	})
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, nodes.ErrNotFound) {
			code = http.StatusNotFound
		}
		abortErr(c, code, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) handleNodeDelete(c *gin.Context) {
	if err := s.nodes.Delete(c.Param("id")); err != nil {
		abortErr(c, http.StatusNotFound, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// handleNodeJoinCode 生成一次性绑定码与子节点上的一行命令。
func (s *Server) handleNodeJoinCode(c *gin.Context) {
	code, exp := s.joinCodes.Create()
	cmd := fmt.Sprintf("curl -fsSL %s | sudo bash -s -- join %s %s",
		installScriptURL, s.panelBaseURL(c), code)
	c.JSON(http.StatusOK, gin.H{"code": code, "expiresAt": exp.Format(time.RFC3339), "command": cmd})
}

type nodeRegisterReq struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

// handleNodeRegister 子节点凭一次性绑定码自注册(公开端点,限流)。
func (s *Server) handleNodeRegister(c *gin.Context) {
	var req nodeRegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if !s.joinCodes.Consume(req.Code) {
		abortErr(c, http.StatusForbidden, "绑定码无效或已过期,请在主节点重新生成")
		return
	}
	u, err := nodes.NormalizeURL(req.URL)
	if err != nil {
		abortErr(c, http.StatusBadRequest, err.Error())
		return
	}
	probe := nodes.Ping(c.Request.Context(), nodes.Node{URL: u, Token: req.Token})
	if !probe.Reachable {
		abortErr(c, http.StatusBadGateway,
			"主节点无法回连子节点("+u+"): "+probe.Error+";请确认子节点面板端口可从主节点访问")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = u
	}
	if _, err := s.nodes.Add(name, u, req.Token, false); err != nil {
		abortErr(c, http.StatusConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// handleNodeProxy 把请求原样转发到子节点 /api/*(含 SSE 流式响应);
// 转发前按操作类别校验该用户在此节点上的授权。
func (s *Server) handleNodeProxy(c *gin.Context) {
	g, ok := grantFor(identity(c), c.Param("id"))
	if !ok {
		abortErr(c, http.StatusForbidden, "没有该节点的管理权限")
		return
	}
	if !permNeeded(c.Request.Method, c.Param("rest"))(g) {
		abortErr(c, http.StatusForbidden, "没有在该节点执行此操作的权限")
		return
	}
	n, err := s.nodes.Get(c.Param("id"))
	if err != nil {
		abortErr(c, http.StatusNotFound, err.Error())
		return
	}
	rest := strings.TrimPrefix(c.Param("rest"), "/")
	target := "/api/" + rest
	if q := c.Request.URL.RawQuery; q != "" {
		target += "?" + q
	}
	req, err := nodes.NewRequest(c.Request.Context(), n, c.Request.Method, target, c.Request.Body)
	if err != nil {
		abortErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	if ct := c.GetHeader("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, err := nodes.HTTPClient(n.InsecureTLS, 0).Do(req) // 超时交由请求上下文控制
	if err != nil {
		abortErr(c, http.StatusBadGateway, "子节点不可达: "+err.Error())
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Disposition", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			c.Header(h, v)
		}
	}
	c.Status(resp.StatusCode)
	buf := make([]byte, 32*1024)
	for {
		nr, rerr := resp.Body.Read(buf)
		if nr > 0 {
			if _, werr := c.Writer.Write(buf[:nr]); werr != nil {
				return
			}
			c.Writer.Flush() // SSE 需要及时刷出
		}
		if rerr != nil {
			return
		}
	}
}

type batchReq struct {
	IDs    []string        `json:"ids"`
	Method string          `json:"method"`
	Path   string          `json:"path"` // 相对 /api/,如 service/openvpn/restart
	Body   json.RawMessage `json:"body"`
}

type batchResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// handleNodeBatch 并发向多个子节点下发同一请求。
func (s *Server) handleNodeBatch(c *gin.Context) {
	var req batchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	switch req.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut:
	default:
		abortErr(c, http.StatusBadRequest, "method 仅允许 GET/POST/PUT")
		return
	}
	if strings.Contains(req.Path, "..") || len(req.IDs) == 0 {
		abortErr(c, http.StatusBadRequest, "参数不合法")
		return
	}

	ident := identity(c)
	results := make([]batchResult, len(req.IDs))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, id := range req.IDs {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := batchResult{ID: id}
			g, ok := grantFor(ident, id)
			if !ok {
				res.Body = "没有该节点的管理权限"
				results[i] = res
				return
			}
			if !permNeeded(req.Method, req.Path)(g) {
				res.Body = "没有在该节点执行此操作的权限"
				results[i] = res
				return
			}
			n, err := s.nodes.Get(id)
			if err != nil {
				res.Body = err.Error()
				results[i] = res
				return
			}
			res.Name = n.Name
			// 升级/apt 操作可能较慢,给足超时
			ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
			defer cancel()
			var body io.Reader
			if len(req.Body) > 0 {
				body = bytes.NewReader(req.Body)
			}
			hreq, err := nodes.NewRequest(ctx, n, req.Method, "/api/"+strings.TrimLeft(req.Path, "/"), body)
			if err != nil {
				res.Body = err.Error()
				results[i] = res
				return
			}
			if body != nil {
				hreq.Header.Set("Content-Type", "application/json")
			}
			resp, err := nodes.HTTPClient(n.InsecureTLS, 5*time.Minute).Do(hreq)
			if err != nil {
				res.Body = "不可达: " + err.Error()
				results[i] = res
				return
			}
			defer resp.Body.Close()
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			res.Status = resp.StatusCode
			res.OK = resp.StatusCode < 400
			res.Body = strings.TrimSpace(string(data))
			results[i] = res
		}(i, id)
	}
	wg.Wait()
	c.JSON(http.StatusOK, gin.H{"results": results})
}
