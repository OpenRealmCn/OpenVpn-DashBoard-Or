package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleDNSStub(c *gin.Context) {
	st, err := s.dns.State(c.Request.Context())
	if err != nil {
		abortErr(c, http.StatusInternalServerError, "获取 DNS 状态失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, st)
}

// handleDNSStubDisable 通过 drop-in 关闭 resolved 的 DNS Stub。
// 只影响 systemd-resolved 自身,不涉及其他进程,因此无需 53 端口决策;
// 原状先落盘为恢复点,失败时立即回滚。
func (s *Server) handleDNSStubDisable(c *gin.Context) {
	ctx := c.Request.Context()
	backup := s.dns.SnapshotStub()
	if err := s.dns.SaveBackup(backup); err != nil {
		abortErr(c, http.StatusInternalServerError, "保存恢复点失败: "+err.Error())
		return
	}
	if err := s.dns.DisableStub(ctx, &backup); err != nil {
		// 失败立即回滚到快照,并清掉恢复点
		_ = s.dns.RevertStub(ctx, backup)
		_ = s.dns.ClearBackup()
		abortErr(c, http.StatusInternalServerError, "关闭 DNS Stub 失败,已回滚: "+err.Error())
		return
	}
	// DisableStub 可能补充了 SwitchedResolvConf 标记,重新落盘
	if err := s.dns.SaveBackup(backup); err != nil {
		abortErr(c, http.StatusInternalServerError, "更新恢复点失败: "+err.Error())
		return
	}
	st, _ := s.dns.State(ctx)
	c.JSON(http.StatusOK, gin.H{"ok": true, "state": st})
}

func (s *Server) handleDNSStubRestore(c *gin.Context) {
	ctx := c.Request.Context()
	backup, err := s.dns.LoadBackup()
	if err != nil {
		abortErr(c, http.StatusNotFound, "没有可用的恢复点")
		return
	}
	if err := s.dns.RevertStub(ctx, backup); err != nil {
		abortErr(c, http.StatusInternalServerError, "恢复失败: "+err.Error())
		return
	}
	_ = s.dns.ClearBackup()
	st, _ := s.dns.State(ctx)
	c.JSON(http.StatusOK, gin.H{"ok": true, "state": st})
}
