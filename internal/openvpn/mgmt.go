package openvpn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// MgmtSocket 是新装实例 server.conf 里配置的管理接口 unix socket。
const MgmtSocket = "/run/openvpn-server/ovpn-mgmt.sock"

// KillClient 通过管理接口断开指定 CN 的所有会话。
// 老配置(无 management 行)会因 socket 不存在而返回友好错误。
func KillClient(ctx context.Context, socketPath, cn string) error {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return errors.New("管理接口不可用(该实例的 server.conf 可能没有 management 配置,重装后支持在线断开)")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	r := bufio.NewReader(conn)
	// 丢弃问候行(">INFO:...")
	if _, err := r.ReadString('\n'); err != nil {
		return fmt.Errorf("读取管理接口问候失败: %w", err)
	}
	if _, err := fmt.Fprintf(conn, "kill %s\n", cn); err != nil {
		return err
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return fmt.Errorf("读取管理接口响应失败: %w", err)
		}
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "SUCCESS:"):
			_, _ = fmt.Fprint(conn, "quit\n")
			return nil
		case strings.HasPrefix(line, "ERROR:"):
			_, _ = fmt.Fprint(conn, "quit\n")
			return errors.New("断开失败: " + strings.TrimPrefix(line, "ERROR:"))
		}
	}
}
