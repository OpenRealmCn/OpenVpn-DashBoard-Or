// Package clients 是客户端证书的业务编排层:创建(有/无密码)、
// 列表、吊销(revoke + gen-crl + 更新 crl.pem)与 .ovpn 生成。
package clients

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"openvpntools/internal/easyrsa"
	"openvpntools/internal/installer"
	"openvpntools/internal/openvpn"
	"openvpntools/internal/platform"
	"openvpntools/internal/store"
)

var (
	cnRe            = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	ErrNotInstalled = errors.New("OpenVPN 尚未通过本面板安装,无法管理客户端")
	ErrBadName      = errors.New("客户端名只允许字母、数字、下划线和连字符(1-64 位)")
	ErrExists       = errors.New("同名客户端已存在")
	ErrNotFound     = errors.New("客户端不存在")
	ErrNotValid     = errors.New("客户端证书不是有效状态")
)

var reservedNames = map[string]bool{"server": true, "ca": true}

type Manager struct {
	plat     platform.Platform
	ez       *easyrsa.EasyRSA
	simulate bool

	mu        sync.Mutex
	simKicked map[string]bool // mock 模式下被"断开"的在线客户端
}

func New(plat platform.Platform, simulate bool) *Manager {
	return &Manager{plat: plat, ez: easyrsa.New(plat), simulate: simulate, simKicked: map[string]bool{}}
}

func (m *Manager) indexPath() string {
	return filepath.Join(m.plat.Paths().PKIDir, "index.txt")
}

func (m *Manager) ownersPath() string {
	return filepath.Join(m.plat.Paths().DataDir, "cert-owners.json")
}

// Record 在证书信息上附加创建者(配额与子用户归属判断用)。
type Record struct {
	easyrsa.CertInfo
	Owner string `json:"owner"`
}

func (m *Manager) loadOwners() map[string]string {
	om := map[string]string{}
	_ = store.LoadJSON(m.ownersPath(), &om)
	return om
}

func (m *Manager) recordOwner(cn, owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	om := m.loadOwners()
	om[cn] = owner
	_ = store.SaveJSON(m.ownersPath(), om)
}

func (m *Manager) List(ctx context.Context) ([]Record, error) {
	fs := m.plat.FS()
	if !fs.Exists(m.indexPath()) {
		return nil, ErrNotInstalled
	}
	data, err := fs.ReadFile(m.indexPath())
	if err != nil {
		return nil, err
	}
	owners := m.loadOwners()
	all := easyrsa.ParseIndex(data)
	out := make([]Record, 0, len(all))
	for _, c := range all {
		if c.CN == "server" { // 服务器自身证书不在客户端列表展示
			continue
		}
		out = append(out, Record{CertInfo: c, Owner: owners[c.CN]})
	}
	return out, nil
}

// CountOwnedValid 统计某用户名下的有效证书数(配额判断)。
func (m *Manager) CountOwnedValid(ctx context.Context, owner string) (int, error) {
	list, err := m.List(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range list {
		if c.Owner == owner && c.Status == easyrsa.StatusValid {
			n++
		}
	}
	return n, nil
}

func (m *Manager) find(ctx context.Context, cn string) (Record, error) {
	list, err := m.List(ctx)
	if err != nil {
		return Record{}, err
	}
	for _, c := range list {
		if c.CN == cn && c.Status == easyrsa.StatusValid {
			return c, nil
		}
	}
	for _, c := range list {
		if c.CN == cn {
			return c, nil
		}
	}
	return Record{}, ErrNotFound
}

// OwnerOf 返回证书创建者;找不到证书时返回 ErrNotFound。
func (m *Manager) OwnerOf(ctx context.Context, cn string) (string, error) {
	c, err := m.find(ctx, cn)
	if err != nil {
		return "", err
	}
	return c.Owner, nil
}

// Create 创建客户端证书;passphrase 非空时私钥加密(导入时需输入);
// expireDays 0 = 默认 825 天;owner 记录创建者用于配额与归属。
func (m *Manager) Create(ctx context.Context, cn, passphrase, owner string, expireDays int) error {
	if !cnRe.MatchString(cn) {
		return ErrBadName
	}
	if reservedNames[strings.ToLower(cn)] {
		return fmt.Errorf("客户端名 %q 是保留名称", cn)
	}
	if expireDays < 0 || expireDays > 3650 {
		return fmt.Errorf("证书有效期需在 1-3650 天之间")
	}
	if expireDays == 0 {
		expireDays = 825
	}
	if !m.plat.FS().Exists(m.indexPath()) {
		return ErrNotInstalled
	}
	if c, err := m.find(ctx, cn); err == nil && c.Status == easyrsa.StatusValid {
		return ErrExists
	}
	var err error
	if m.simulate {
		err = m.simulateCreate(cn, expireDays)
	} else {
		err = m.ez.BuildClientFull(ctx, cn, passphrase, expireDays)
	}
	if err != nil {
		return err
	}
	m.recordOwner(cn, owner)
	return nil
}

// Revoke 吊销证书并刷新 CRL;OpenVPN 对新连接自动重读 crl.pem,无需重启。
func (m *Manager) Revoke(ctx context.Context, cn string) error {
	c, err := m.find(ctx, cn)
	if err != nil {
		return err
	}
	if c.Status != easyrsa.StatusValid {
		return ErrNotValid
	}
	if m.simulate {
		return m.simulateRevoke(cn)
	}
	if err := m.ez.Revoke(ctx, cn); err != nil {
		return err
	}
	return m.refreshCRL()
}

// refreshCRL 把新 CRL 原子拷贝到 openvpn 目录(0644,降权进程可读)。
func (m *Manager) refreshCRL() error {
	fs := m.plat.FS()
	src := filepath.Join(m.plat.Paths().PKIDir, "crl.pem")
	data, err := fs.ReadFile(src)
	if err != nil {
		return fmt.Errorf("读取新 CRL 失败: %w", err)
	}
	dst := filepath.Join(m.plat.Paths().ServerConfDir, "crl.pem")
	return fs.WriteFileAtomic(dst, data, 0o644)
}

// Profile 生成内联 .ovpn;返回内容与建议文件名。
func (m *Manager) Profile(ctx context.Context, cn string) ([]byte, string, error) {
	c, err := m.find(ctx, cn)
	if err != nil {
		return nil, "", err
	}
	if c.Status != easyrsa.StatusValid {
		return nil, "", ErrNotValid
	}
	params, err := installer.LoadParams(m.plat.Paths().DataDir)
	if err != nil {
		return nil, "", errors.New("找不到安装参数(install.json),无法确定 remote 地址")
	}
	fs := m.plat.FS()
	pki := m.plat.Paths().PKIDir
	read := func(rel string) (string, error) {
		data, err := fs.ReadFile(filepath.Join(pki, rel))
		if err != nil {
			return "", fmt.Errorf("读取 %s 失败: %w", rel, err)
		}
		return string(data), nil
	}
	ca, err := read("ca.crt")
	if err != nil {
		return nil, "", err
	}
	cert, err := read(filepath.Join("issued", cn+".crt"))
	if err != nil {
		return nil, "", err
	}
	key, err := read(filepath.Join("private", cn+".key"))
	if err != nil {
		return nil, "", err
	}
	ta, err := read("ta.key")
	if err != nil {
		return nil, "", err
	}
	conf := openvpn.RenderClientConf(openvpn.ClientConfParams{
		Remote:   params.PublicAddr,
		Port:     params.Port,
		Proto:    params.Proto,
		CA:       openvpn.ExtractPEM(ca),
		Cert:     openvpn.ExtractPEM(cert),
		Key:      strings.TrimSpace(key),
		TLSCrypt: strings.TrimSpace(ta),
	})
	return []byte(conf), cn + ".ovpn", nil
}

// —— mock 模式:手工维护 index.txt 与占位文件,供前端联调 ——

func (m *Manager) simulateCreate(cn string, expireDays int) error {
	fs := m.plat.FS()
	pki := m.plat.Paths().PKIDir
	files := map[string]string{
		filepath.Join(pki, "issued", cn+".crt"):  "-----BEGIN CERTIFICATE-----\nMOCK-CERT-" + cn + "\n-----END CERTIFICATE-----",
		filepath.Join(pki, "private", cn+".key"): "-----BEGIN PRIVATE KEY-----\nMOCK-KEY-" + cn + "\n-----END PRIVATE KEY-----",
	}
	for p, content := range files {
		if err := fs.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := fs.WriteFileAtomic(p, []byte(content), 0o600); err != nil {
			return err
		}
	}
	expiry := time.Now().AddDate(0, 0, expireDays).UTC().Format("060102150405Z")
	serial := fmt.Sprintf("%X", time.Now().UnixNano())
	line := fmt.Sprintf("V\t%s\t\t%s\tunknown\t/CN=%s\n", expiry, serial, cn)
	data, _ := fs.ReadFile(m.indexPath())
	return fs.WriteFileAtomic(m.indexPath(), append(data, []byte(line)...), 0o600)
}

func (m *Manager) simulateRevoke(cn string) error {
	fs := m.plat.FS()
	data, err := fs.ReadFile(m.indexPath())
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format("060102150405Z")
	lines := strings.Split(string(data), "\n")
	for i, ln := range lines {
		f := strings.Split(ln, "\t")
		if len(f) >= 6 && strings.TrimSpace(f[0]) == "V" && strings.HasSuffix(f[5], "/CN="+cn) {
			f[0], f[2] = "R", now
			lines[i] = strings.Join(f, "\t")
			break
		}
	}
	return fs.WriteFileAtomic(m.indexPath(), []byte(strings.Join(lines, "\n")), 0o600)
}
