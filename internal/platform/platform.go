// Package platform 是所有系统操作的唯一入口:handler、installer 等上层代码
// 不得直接 exec 或读写系统路径,必须经由 Platform 接口,
// 以便在 Windows 开发环境用 mock 实现联调。
package platform

import (
	"context"
	"io"
	"os"
	"time"
)

type ServiceAction string

const (
	ActStart        ServiceAction = "start"
	ActStop         ServiceAction = "stop"
	ActRestart      ServiceAction = "restart"
	ActEnable       ServiceAction = "enable"
	ActDisable      ServiceAction = "disable"
	ActEnableNow    ServiceAction = "enable-now"
	ActDisableNow   ServiceAction = "disable-now"
	ActDaemonReload ServiceAction = "daemon-reload"
)

type ServiceStatus struct {
	Exists  bool `json:"exists"`
	Active  bool `json:"active"`
	Enabled bool `json:"enabled"`
}

type PortInfo struct {
	Proto string `json:"proto"` // tcp / udp
	Addr  string `json:"addr"`
	Port  int    `json:"port"`
	PID   int    `json:"pid"`
	Comm  string `json:"comm"`
	Unit  string `json:"unit"` // 由 PID 反查到的 systemd unit,可为空
}

type OSInfo struct {
	ID        string `json:"id"` // ubuntu / debian
	VersionID string `json:"versionId"`
	Pretty    string `json:"pretty"`
}

type RunOpt struct {
	Argv       []string   // argv[0] 为程序名;绝不经过 shell,杜绝注入
	Env        []string   // 追加到 os.Environ()
	Stdin      io.Reader
	ExtraFiles []*os.File // 从 fd 3 起映射,证书密码经管道传入用
	Dir        string
	Timeout    time.Duration // 0 = 默认 5 分钟
}

type RunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type FS interface {
	ReadFile(path string) ([]byte, error)
	WriteFileAtomic(path string, data []byte, perm os.FileMode) error
	Remove(path string) error
	RemoveAll(path string) error
	MkdirAll(path string, perm os.FileMode) error
	Rename(oldPath, newPath string) error
	Symlink(target, link string) error
	Readlink(link string) (string, error)
	Exists(path string) bool
}

type Paths struct {
	DataDir         string // 面板数据目录(journal 等)
	EasyRSADir      string // /etc/openvpn/easy-rsa
	PKIDir          string // easy-rsa/pki
	ServerConfDir   string // /etc/openvpn/server
	SysctlDir       string // /etc/sysctl.d
	ResolvedDropDir string // /etc/systemd/resolved.conf.d
	ResolvConf      string // /etc/resolv.conf
}

type Platform interface {
	OSInfo(ctx context.Context) (OSInfo, error)
	ServiceCtl(ctx context.Context, unit string, act ServiceAction) error
	ServiceStatus(ctx context.Context, unit string) (ServiceStatus, error)
	ListenPorts(ctx context.Context) ([]PortInfo, error)
	ReadSysctl(key string) (string, error)
	WriteSysctlD(file, key, value string) error
	AptInstall(ctx context.Context, pkgs ...string) error
	IsPkgInstalled(ctx context.Context, pkg string) (bool, error)
	Run(ctx context.Context, opt RunOpt) (RunResult, error)
	FS() FS
	Paths() Paths
}
