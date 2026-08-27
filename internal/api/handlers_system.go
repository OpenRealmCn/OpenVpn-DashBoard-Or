package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleFixIPForward 写 /etc/sysctl.d/ 持久化并即时生效,不修改 /etc/sysctl.conf。
func (s *Server) handleFixIPForward(c *gin.Context) {
	if err := s.plat.WriteSysctlD(sysctlDropIn, "net.ipv4.ip_forward", "1"); err != nil {
		abortErr(c, http.StatusInternalServerError, "开启 IPv4 转发失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
