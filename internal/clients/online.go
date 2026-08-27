package clients

import (
	"context"
	"fmt"
	"time"

	"openvpntools/internal/easyrsa"
	"openvpntools/internal/openvpn"
)

// Online 返回当前已连接的客户端(解析 status.log,服务端约 60 秒刷新一次)。
func (m *Manager) Online(ctx context.Context) ([]openvpn.OnlineClient, error) {
	if m.simulate {
		return m.simulateOnline(ctx)
	}
	fs := m.plat.FS()
	if !fs.Exists(openvpn.StatusLogPath) {
		return []openvpn.OnlineClient{}, nil // 服务未运行或尚未产生状态文件
	}
	data, err := fs.ReadFile(openvpn.StatusLogPath)
	if err != nil {
		return nil, fmt.Errorf("读取状态文件失败: %w", err)
	}
	return openvpn.ParseStatusLog(data), nil
}

// Kick 通过管理接口断开在线客户端(证书仍有效,可重连;永久禁用请用吊销)。
func (m *Manager) Kick(ctx context.Context, cn string) error {
	if _, err := m.find(ctx, cn); err != nil {
		return err
	}
	if m.simulate {
		m.mu.Lock()
		m.simKicked[cn] = true
		m.mu.Unlock()
		return nil
	}
	return openvpn.KillClient(ctx, openvpn.MgmtSocket, cn)
}

// simulateOnline:mock 模式取前几个有效证书,按固定规律编造流量与地址。
func (m *Manager) simulateOnline(ctx context.Context) ([]openvpn.OnlineClient, error) {
	list, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []openvpn.OnlineClient{}
	n := 0
	for _, c := range list {
		if c.Status != easyrsa.StatusValid || m.simKicked[c.CN] {
			continue
		}
		n++
		out = append(out, openvpn.OnlineClient{
			CN:          c.CN,
			RealAddr:    fmt.Sprintf("198.51.100.%d:%d", 10+n, 50000+n),
			VirtualAddr: fmt.Sprintf("10.8.0.%d", n+1),
			BytesRecv:   int64(n) * 3_412_007,
			BytesSent:   int64(n) * 27_895_113,
			Since:       time.Now().Add(-time.Duration(n) * 37 * time.Minute),
		})
		if n == 3 {
			break
		}
	}
	return out, nil
}
