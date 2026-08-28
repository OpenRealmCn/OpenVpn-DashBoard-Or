package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/nodes"
)

const installScriptURL = "https://raw.githubusercontent.com/OpenRealmCn/OpenVpn-DashBoard-Or/main/install.sh"

// panelBaseURL 返回子节点回连本面板的地址:优先「面板设置」里的 PanelURL,
// 否则按当前请求的协议与 Host 推断。
func (s *Server) panelBaseURL(c *gin.Context) string {
	if base := s.cfg.Snapshot().PanelURL; base != "" {
		return base
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

// handleSSHInstallStart 管理员提供一次性 SSH 凭据,由主节点登录目标机
// 执行 install.sh join 完成安装与绑定。凭据不落盘,任务日志可轮询。
func (s *Server) handleSSHInstallStart(c *gin.Context) {
	var p nodes.SSHInstallParams
	if err := c.ShouldBindJSON(&p); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	code, _ := s.joinCodes.Create()
	snap, err := s.sshJobs.Start(p, nodes.JoinTarget{
		PanelURL:  s.panelBaseURL(c),
		Code:      code,
		ScriptURL: installScriptURL,
		Mirror:    s.cfg.Snapshot().GithubMirror,
	}, s.mode == "mock")
	if err != nil {
		abortErr(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobId": snap.ID})
}

// handleSSHInstallStatus 轮询任务状态与日志(?job=<id>)。
func (s *Server) handleSSHInstallStatus(c *gin.Context) {
	job, ok := s.sshJobs.Get(c.Query("job"))
	if !ok {
		abortErr(c, http.StatusNotFound, "任务不存在或已过期")
		return
	}
	c.JSON(http.StatusOK, job.Snapshot())
}
