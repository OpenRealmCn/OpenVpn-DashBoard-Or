package api

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"openvpntools/internal/backup"
	"openvpntools/internal/openvpn"
	"openvpntools/internal/platform"
)

// handleRestore 从上传的备份 tar.gz 恢复;被替换的现状留底在数据目录,
// 恢复后重载子用户并尝试重启 OpenVPN。
func (s *Server) handleRestore(c *gin.Context) {
	if job := s.engine.CurrentJob(); job != nil && job.Snapshot().State == "running" {
		abortErr(c, http.StatusConflict, "安装进行中,不能执行恢复")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 200<<20)
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		abortErr(c, http.StatusBadRequest, "请上传备份文件(字段名 file)")
		return
	}
	defer file.Close()

	sum, err := backup.Restore(s.plat, file)
	if err != nil {
		abortErr(c, http.StatusBadRequest, "恢复失败: "+err.Error())
		return
	}
	if err := s.users.Reload(); err != nil {
		log.Printf("恢复后重载子用户失败: %v", err)
	}
	note := "未检测到 OpenVPN 服务,跳过重启"
	if st, err := s.plat.ServiceStatus(c.Request.Context(), openvpn.ServiceUnit); err == nil && st.Exists {
		if err := s.plat.ServiceCtl(c.Request.Context(), openvpn.ServiceUnit, platform.ActRestart); err != nil {
			note = "OpenVPN 重启失败: " + err.Error()
		} else {
			note = "OpenVPN 已重启,恢复的配置已生效"
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "summary": sum, "note": note})
}

// handleBackup 导出运维备份(tar.gz):PKI(CA/证书/index)、server 配置、
// 安装参数、子用户与证书归属。面板 config.yaml(含密钥)需自行备份。
func (s *Server) handleBackup(c *gin.Context) {
	paths := s.plat.Paths()
	name := "openvpntools-backup-" + time.Now().Format("20060102-150405") + ".tar.gz"
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	c.Header("Content-Type", "application/gzip")
	c.Status(http.StatusOK)

	gz := gzip.NewWriter(c.Writer)
	tw := tar.NewWriter(gz)
	defer func() {
		_ = tw.Close()
		_ = gz.Close()
	}()

	if err := addDirToTar(tw, paths.PKIDir, "pki"); err != nil {
		log.Printf("备份 pki 失败: %v", err)
		return
	}
	if err := addDirToTar(tw, paths.ServerConfDir, "openvpn-server"); err != nil {
		log.Printf("备份 server 配置失败: %v", err)
		return
	}
	for _, f := range []string{"install.json", "users.json", "cert-owners.json"} {
		if err := addFileToTar(tw, filepath.Join(paths.DataDir, f), "data/"+f); err != nil {
			log.Printf("备份 %s 失败: %v", f, err)
			return
		}
	}
}

func addDirToTar(tw *tar.Writer, srcDir, prefix string) error {
	if _, err := os.Stat(srcDir); err != nil {
		return nil // 目录不存在(未安装)则跳过
	}
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		hdr := &tar.Header{
			Name:    prefix + "/" + strings.ReplaceAll(rel, string(os.PathSeparator), "/"),
			Mode:    int64(info.Mode() & 0o777),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	})
}

func addFileToTar(tw *tar.Writer, path, name string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil // 可选文件缺失直接跳过
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}
