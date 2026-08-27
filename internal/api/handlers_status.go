package api

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/openvpn"
)

// sysctlDropIn 是本工具写入 ip_forward 持久化配置的文件名。
const sysctlDropIn = "99-openvpntools.conf"

func (s *Server) handleStatus(c *gin.Context) {
	ctx := c.Request.Context()

	svc, svcErr := s.plat.ServiceStatus(ctx, openvpn.ServiceUnit)
	ports, portsErr := s.plat.ListenPorts(ctx)
	osi, _ := s.plat.OSInfo(ctx)

	ipfwd, _ := s.plat.ReadSysctl("net.ipv4.ip_forward")
	persisted := s.plat.FS().Exists(filepath.Join(s.plat.Paths().SysctlDir, sysctlDropIn))
	dnsState, _ := s.dns.State(ctx)

	c.JSON(http.StatusOK, gin.H{
		"mode": s.mode,
		"os":   osi,
		"openvpn": gin.H{
			"unit":    openvpn.ServiceUnit,
			"service": svc,
			"error":   errStr(svcErr),
		},
		"ports":      ports,
		"portsError": errStr(portsErr),
		"ipForward": gin.H{
			"runtime":   ipfwd == "1",
			"persisted": persisted,
		},
		"dns": dnsState,
	})
}
