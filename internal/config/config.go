package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"gopkg.in/yaml.v3"

	"openvpntools/internal/store"
)

type TLS struct {
	Enabled  bool   `yaml:"enabled"` // 兼容旧配置:true 等价 mode: self
	Mode     string `yaml:"mode"`    // off / self / le(Let's Encrypt)
	Domain   string `yaml:"domain"`  // le 模式必填,需解析到本机
	Email    string `yaml:"email"`   // le 可选,证书到期提醒
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// EffectiveMode 归一化 TLS 模式(空 Mode 按旧字段 Enabled 推导)。
func (t TLS) EffectiveMode() string {
	switch t.Mode {
	case "off", "self", "le":
		return t.Mode
	}
	if t.Enabled {
		return "self"
	}
	return "off"
}

type Config struct {
	Listen       string `yaml:"listen"`        // 面板监听地址
	DataDir      string `yaml:"data_dir"`      // journal、job 状态等数据目录
	PanelURL     string `yaml:"panel_url"`     // 二维码外部地址,空则回退请求 Host
	GithubMirror string `yaml:"github_mirror"` // GitHub 下载镜像前缀,空则直连
	JWTSecret    string `yaml:"jwt_secret"`    // 首启自动生成
	AdminHash    string `yaml:"admin_hash"`    // bcrypt;空 = 未初始化
	TLS          TLS    `yaml:"tls"`
}

func DefaultPath() string {
	if runtime.GOOS == "linux" {
		return "/etc/openvpntools/config.yaml"
	}
	return filepath.Join("devdata", "config.yaml")
}

func defaults() Config {
	c := Config{Listen: "0.0.0.0:8686"}
	if runtime.GOOS == "linux" {
		c.DataDir = "/var/lib/openvpntools"
	} else {
		c.DataDir = "devdata"
	}
	return c
}

// Manager 持有配置的唯一可变副本,所有修改经 Update 原子落盘。
type Manager struct {
	mu   sync.RWMutex
	path string
	c    Config
}

func Load(path string) (*Manager, error) {
	m := &Manager{path: path, c: defaults()}
	data, err := os.ReadFile(path)
	fresh := false
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, &m.c); err != nil {
			return nil, err
		}
	case errors.Is(err, os.ErrNotExist):
		fresh = true
	default:
		return nil, err
	}
	changed := false
	if m.c.JWTSecret == "" {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		m.c.JWTSecret = hex.EncodeToString(buf)
		changed = true
	}
	if fresh || changed {
		if err := m.saveLocked(); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) Snapshot() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.c
}

func (m *Manager) Update(fn func(*Config) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.c
	if err := fn(&next); err != nil {
		return err
	}
	m.c = next
	return m.saveLocked()
}

func (m *Manager) saveLocked() error {
	data, err := yaml.Marshal(&m.c)
	if err != nil {
		return err
	}
	return store.AtomicWriteFile(m.path, data, 0o600)
}
