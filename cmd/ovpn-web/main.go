package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"path/filepath"

	"golang.org/x/crypto/acme/autocert"

	"openvpntools/internal/api"
	"openvpntools/internal/audit"
	"openvpntools/internal/auth"
	"openvpntools/internal/config"
	"openvpntools/internal/platform"
	"openvpntools/internal/platform/linux"
	"openvpntools/internal/platform/mock"
	"openvpntools/internal/tlsutil"
	"openvpntools/internal/users"
	"openvpntools/web"
)

func main() {
	cfgPath := flag.String("config", config.DefaultPath(), "配置文件路径")
	flag.Parse()

	cfgMgr, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败(%s): %v", *cfgPath, err)
	}
	cfg := cfgMgr.Snapshot()

	// Linux 上默认走真实现;设置 OVPN_MOCK=1 或非 Linux 平台走 mock,便于本地联调。
	var plat platform.Platform
	mode := "mock"
	if runtime.GOOS == "linux" && os.Getenv("OVPN_MOCK") == "" {
		plat = linux.New(cfg.DataDir)
		mode = "linux"
	} else {
		plat = mock.New(cfg.DataDir)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	usersStore, err := users.Load(filepath.Join(cfg.DataDir, "users.json"))
	if err != nil {
		log.Fatalf("加载子用户失败: %v", err)
	}
	auditLog := audit.New(filepath.Join(cfg.DataDir, "audit.log"))

	authSvc := auth.New(cfgMgr, usersStore)
	static, embedded := web.FS()
	router := api.New(cfgMgr, authSvc, usersStore, plat, auditLog, mode).Router(static)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	tlsMode := cfg.TLS.EffectiveMode()
	certFile, keyFile := cfg.TLS.CertFile, cfg.TLS.KeyFile
	switch tlsMode {
	case "self":
		// 未指定证书时自动生成自签名证书
		if certFile == "" || keyFile == "" {
			certFile = filepath.Join(cfg.DataDir, "panel-cert.pem")
			keyFile = filepath.Join(cfg.DataDir, "panel-key.pem")
			if err := tlsutil.EnsureSelfSigned(certFile, keyFile); err != nil {
				log.Fatalf("生成自签名证书失败: %v", err)
			}
		}
	case "le":
		if cfg.TLS.Domain == "" {
			log.Fatalf("Let's Encrypt 模式必须在设置中填写域名")
		}
		mgr := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(filepath.Join(cfg.DataDir, "acme")),
			HostPolicy: autocert.HostWhitelist(cfg.TLS.Domain),
			Email:      cfg.TLS.Email,
		}
		// ACME 要求 443(TLS-ALPN)与 80(HTTP-01/跳转)
		if cfg.Listen != ":443" && cfg.Listen != "0.0.0.0:443" {
			log.Printf("Let's Encrypt 模式强制监听 :443(忽略配置的 %s)", cfg.Listen)
		}
		srv.Addr = ":443"
		srv.TLSConfig = mgr.TLSConfig()
		go func() {
			if err := http.ListenAndServe(":80", mgr.HTTPHandler(nil)); err != nil {
				log.Printf("80 端口监听失败(HTTP-01 验证与跳转不可用): %v", err)
			}
		}()
	}

	go func() {
		scheme := "http"
		if tlsMode != "off" {
			scheme = "https"
		}
		log.Printf("OpenVpnTools 面板已启动: %s://%s (tls=%s, platform=%s, 内嵌前端=%v, 配置=%s)",
			scheme, srv.Addr, tlsMode, mode, embedded, *cfgPath)
		var err error
		switch tlsMode {
		case "self":
			err = srv.ListenAndServeTLS(certFile, keyFile)
		case "le":
			err = srv.ListenAndServeTLS("", "") // 证书由 autocert 提供
		default:
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务异常退出: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	log.Println("面板已退出")
}
