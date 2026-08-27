package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/installer"
)

func (s *Server) handleInstallState(c *gin.Context) {
	resp := gin.H{
		"installed":      s.engine.Installed(),
		"pendingJournal": s.engine.PendingJournal(),
	}
	if job := s.engine.CurrentJob(); job != nil {
		resp["job"] = job.Snapshot()
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Server) handleInstallPrecheck(c *gin.Context) {
	var p installer.Params
	if err := c.ShouldBindJSON(&p); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	rep, err := s.engine.Precheck(c.Request.Context(), p)
	if err != nil {
		abortErr(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, rep)
}

func (s *Server) handleInstallStart(c *gin.Context) {
	var p installer.Params
	if err := c.ShouldBindJSON(&p); err != nil {
		abortErr(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	jobID, err := s.engine.Start(p)
	if err != nil {
		abortErr(c, http.StatusConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobId": jobID})
}

func (s *Server) handleInstallRollback(c *gin.Context) {
	logs, failures, err := s.engine.RollbackPending(c.Request.Context())
	if err != nil {
		abortErr(c, http.StatusConflict, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": len(failures) == 0, "logs": logs, "failures": failures})
}

// handleInstallEvents 以 SSE 推送安装日志;断线重连按 Last-Event-ID 补发。
func (s *Server) handleInstallEvents(c *gin.Context) {
	job := s.engine.CurrentJob()
	if job == nil {
		abortErr(c, http.StatusNotFound, "当前没有安装任务")
		return
	}
	after := 0
	if v := c.GetHeader("Last-Event-ID"); v != "" {
		after, _ = strconv.Atoi(v)
	} else if v := c.Query("after"); v != "" {
		after, _ = strconv.Atoi(v)
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")

	lastSent := after
	send := func(e installer.LogEvent) {
		data, _ := json.Marshal(e)
		fmt.Fprintf(c.Writer, "id: %d\nevent: log\ndata: %s\n\n", e.Seq, data)
		lastSent = e.Seq
	}
	drain := func() {
		for _, e := range job.Logs.Snapshot(lastSent) {
			send(e)
		}
		c.Writer.Flush()
	}

	ch, cancel := job.Logs.Subscribe()
	defer cancel()
	drain()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-job.Done():
			drain()
			snap, _ := json.Marshal(job.Snapshot())
			fmt.Fprintf(c.Writer, "event: state\ndata: %s\n\n", snap)
			c.Writer.Flush()
			return
		case e := <-ch:
			// 订阅通道可能因消费慢而丢事件,统一以 Snapshot 按序补齐
			if e.Seq > lastSent {
				drain()
			}
		}
	}
}
