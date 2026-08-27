// Package updates 提供版本信息与更新:OpenVPN 走 apt 升级,
// EasyRSA 从 GitHub release 升级(强制 GitHub API 提供的 SHA256 digest 校验,
// bash -n 检查,保留 pki,失败恢复旧目录)。
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"openvpntools/internal/config"
	"openvpntools/internal/download"
	"openvpntools/internal/platform"
	"openvpntools/internal/version"
)

const (
	releaseAPI  = "https://api.github.com/repos/OpenVPN/easy-rsa/releases/latest"
	panelAPI    = "https://api.github.com/repos/OpenRealmCn/OpenVpn-DashBoard-Or/releases/latest"
	markerFile  = ".openvpntools-version" // easy-rsa 目录内的版本标记
	openvpnUnit = "openvpn-server@server.service"
)

type Info struct {
	Panel          string `json:"panel"`
	PanelLatest    string `json:"panelLatest"` // 远端最新,空 = 未检查/检查失败
	OpenVPN        string `json:"openvpn"`        // 当前版本,空 = 未安装
	OpenVPNUpgrade string `json:"openvpnUpgrade"` // 可升级到的版本,空 = 已最新/未知
	EasyRSA        string `json:"easyrsa"`        // 当前版本(marker),空 = 未安装
	EasyRSALatest  string `json:"easyrsaLatest"`  // 远端最新,空 = 未检查
	CheckedRemote  bool   `json:"checkedRemote"`
}

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"` // 形如 sha256:xxxx
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type Manager struct {
	plat     platform.Platform
	cfg      *config.Manager
	simulate bool

	mu          sync.Mutex
	latest      *release // EasyRSA 最新 release 缓存,升级时复用
	latestPanel *release // 面板最新 release 缓存
}

func New(plat platform.Platform, cfg *config.Manager, simulate bool) *Manager {
	return &Manager{plat: plat, cfg: cfg, simulate: simulate}
}

var ovpnFullVerRe = regexp.MustCompile(`OpenVPN (\d+\.\d+(?:\.\d+)?)`)
var aptInstRe = regexp.MustCompile(`(?m)^Inst openvpn \[[^\]]*\] \(([^\s)]+)`)

func (u *Manager) markerPath() string {
	return filepath.Join(u.plat.Paths().EasyRSADir, markerFile)
}

// Info 汇总版本;checkRemote 时执行 apt-get update 与 GitHub API 查询(较慢)。
func (u *Manager) Info(ctx context.Context, checkRemote bool) Info {
	info := Info{Panel: version.Panel, CheckedRemote: checkRemote}

	if u.simulate {
		info.OpenVPN = "2.6.12(mock)"
		info.EasyRSA = u.readMarker()
		if info.EasyRSA == "" {
			info.EasyRSA = download.EasyRSAVersion
		}
		if checkRemote {
			info.OpenVPNUpgrade = "2.6.14(mock)"
			info.EasyRSALatest = "3.3.0"
			info.PanelLatest = "0.99.0"
			u.mu.Lock()
			u.latest = &release{TagName: "v3.3.0"} // mock 升级用
			u.mu.Unlock()
		}
		return info
	}

	if res, err := u.plat.Run(ctx, platform.RunOpt{Argv: []string{"openvpn", "--version"}, Timeout: 15 * time.Second}); err == nil {
		if m := ovpnFullVerRe.FindStringSubmatch(res.Stdout); m != nil {
			info.OpenVPN = m[1]
		}
	}
	info.EasyRSA = u.readMarker()

	if checkRemote {
		if v, err := u.aptUpgradable(ctx); err == nil {
			info.OpenVPNUpgrade = v
		}
		if rel, err := u.fetchLatest(ctx); err == nil {
			info.EasyRSALatest = strings.TrimPrefix(rel.TagName, "v")
		}
		if rel, err := u.fetchPanelLatest(ctx); err == nil {
			info.PanelLatest = strings.TrimPrefix(rel.TagName, "v")
		}
	}
	return info
}

func (u *Manager) readMarker() string {
	data, err := u.plat.FS().ReadFile(u.markerPath())
	if err != nil {
		// 本工具安装但缺 marker(旧数据)→ 按内置版本报告
		if u.plat.FS().Exists(filepath.Join(u.plat.Paths().EasyRSADir, "easyrsa")) {
			return download.EasyRSAVersion
		}
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (u *Manager) aptUpgradable(ctx context.Context) (string, error) {
	env := []string{"DEBIAN_FRONTEND=noninteractive"}
	if res, err := u.plat.Run(ctx, platform.RunOpt{Argv: []string{"apt-get", "update"}, Env: env, Timeout: 5 * time.Minute}); err != nil || res.ExitCode != 0 {
		return "", errors.New("apt-get update 失败")
	}
	res, err := u.plat.Run(ctx, platform.RunOpt{
		Argv: []string{"apt-get", "-s", "install", "--only-upgrade", "openvpn"},
		Env:  env, Timeout: time.Minute,
	})
	if err != nil || res.ExitCode != 0 {
		return "", errors.New("apt 模拟升级失败")
	}
	if m := aptInstRe.FindStringSubmatch(res.Stdout); m != nil {
		return m[1], nil
	}
	return "", nil // 已最新
}

func fetchRelease(ctx context.Context, apiURL string) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := download.NewClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("访问 GitHub API 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func (u *Manager) fetchLatest(ctx context.Context) (*release, error) {
	rel, err := fetchRelease(ctx, releaseAPI)
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	u.latest = rel
	u.mu.Unlock()
	return rel, nil
}

func (u *Manager) fetchPanelLatest(ctx context.Context) (*release, error) {
	rel, err := fetchRelease(ctx, panelAPI)
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	u.latestPanel = rel
	u.mu.Unlock()
	return rel, nil
}

// UpgradeOpenVPN 通过 apt 升级并在服务原本运行时重启它。
func (u *Manager) UpgradeOpenVPN(ctx context.Context, logf func(string, ...any)) error {
	if u.simulate {
		logf("(mock) apt-get install --only-upgrade -y openvpn")
		logf("(mock) 已升级到 2.6.14")
		return nil
	}
	wasActive := false
	if st, err := u.plat.ServiceStatus(ctx, openvpnUnit); err == nil {
		wasActive = st.Active
	}
	logf("apt-get install --only-upgrade -y openvpn …")
	res, err := u.plat.Run(ctx, platform.RunOpt{
		Argv: []string{"apt-get", "install", "--only-upgrade", "-y", "openvpn"},
		Env:  []string{"DEBIAN_FRONTEND=noninteractive"},
		Timeout: 10 * time.Minute,
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("apt 升级失败: %s", tail(res.Stderr, 500))
	}
	if wasActive {
		logf("重启 %s …", openvpnUnit)
		if err := u.plat.ServiceCtl(ctx, openvpnUnit, platform.ActRestart); err != nil {
			return fmt.Errorf("升级成功但重启服务失败: %w", err)
		}
	}
	logf("OpenVPN 升级完成")
	return nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
