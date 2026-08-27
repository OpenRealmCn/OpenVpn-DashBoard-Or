// Package openvpn 封装 OpenVPN 服务端:单元名、配置渲染与服务控制。
// M3/M4 里程碑会补充 server.conf 模板与 .ovpn 渲染。
package openvpn

const (
	// ServiceUnit 使用 /etc/openvpn/server/ 目录布局的 systemd 模板单元。
	ServiceUnit = "openvpn-server@server.service"
	DefaultPort = 1194
)
