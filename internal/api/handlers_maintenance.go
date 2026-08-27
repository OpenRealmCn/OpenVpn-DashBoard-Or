package api

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/openvpn"
	"openvpntools/internal/platform"
)

func (s *Server) handleClientsOnline(c *gin.Context) {
	list, err := s.clients.Online(c.Request.Context())
	if err != nil {
		abortErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"online": list})
}

func (s *Server) handleClientKick(c *gin.Context) {
	cn := c.Param("cn")
	if err := s.clients.Kick(c.Request.Context(), cn); err != nil {
		abortErr(c, http.StatusConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// handleServiceCtl 只允许控制 OpenVPN 服务本身,动作白名单。
func (s *Server) handleServiceCtl(c *gin.Context) {
	var act platform.ServiceAction
	switch c.Param("action") {
	case "start":
		act = platform.ActStart
	case "stop":
		act = platform.ActStop
	case "restart":
		act = platform.ActRestart
	default:
		abortErr(c, http.StatusBadRequest, "不支持的操作")
		return
	}
	if err := s.plat.ServiceCtl(c.Request.Context(), openvpn.ServiceUnit, act); err != nil {
		abortErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	st, _ := s.plat.ServiceStatus(c.Request.Context(), openvpn.ServiceUnit)
	c.JSON(http.StatusOK, gin.H{"ok": true, "service": st})
}

// handleVersion:?check=1 时联网检查(apt-get update + GitHub API),否则只报本地版本。
func (s *Server) handleVersion(c *gin.Context) {
	check, _ := strconv.ParseBool(c.Query("check"))
	c.JSON(http.StatusOK, s.updates.Info(c.Request.Context(), check))
}

func (s *Server) handleUpgradeOpenVPN(c *gin.Context) {
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, sprintf(format, args...)) }
	if err := s.updates.UpgradeOpenVPN(c.Request.Context(), logf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "logs": logs})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "logs": logs})
}

func (s *Server) handleUpgradeEasyRSA(c *gin.Context) {
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, sprintf(format, args...)) }
	if err := s.updates.UpgradeEasyRSA(c.Request.Context(), logf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "logs": logs})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "logs": logs})
}

func (s *Server) handleUpgradePanel(c *gin.Context) {
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, sprintf(format, args...)) }
	if err := s.updates.SelfUpdate(c.Request.Context(), logf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "logs": logs})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "logs": logs, "needRestart": true})
}

// handlePanelRestart 干净退出交由 systemd(Restart=always)拉起新进程。
func (s *Server) handlePanelRestart(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true, "note": "面板将在 1 秒后重启"})
	go func() {
		time.Sleep(time.Second)
		os.Exit(0)
	}()
}
