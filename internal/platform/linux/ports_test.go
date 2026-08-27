package linux

import "testing"

const ssFixture = `udp   UNCONN 0      0        127.0.0.53%lo:53         0.0.0.0:*    users:(("systemd-resolve",pid=339,fd=13))
udp   UNCONN 0      0            127.0.0.1:53         0.0.0.0:*    users:(("dnsmasq",pid=812,fd=4))
udp   UNCONN 0      0              0.0.0.0:1194       0.0.0.0:*    users:(("openvpn",pid=1900,fd=6))
tcp   LISTEN 0      128            0.0.0.0:22         0.0.0.0:*    users:(("sshd",pid=800,fd=3))
tcp   LISTEN 0      511                  *:8686             *:*    users:(("ovpn-web",pid=2222,fd=7))
tcp   LISTEN 0      100          [::1]:631              [::]:*
garbage line
`

func TestParseSS(t *testing.T) {
	units := map[int]string{339: "systemd-resolved.service", 812: "dnsmasq.service"}
	ports := ParseSS(ssFixture, func(pid int) string { return units[pid] })

	if len(ports) != 6 {
		t.Fatalf("期望解析出 6 条,实际 %d 条: %+v", len(ports), ports)
	}

	p := ports[0]
	if p.Proto != "udp" || p.Port != 53 || p.Comm != "systemd-resolve" ||
		p.PID != 339 || p.Unit != "systemd-resolved.service" || p.Addr != "127.0.0.53%lo" {
		t.Errorf("resolved 条目解析错误: %+v", p)
	}
	if ports[1].Comm != "dnsmasq" || ports[1].Unit != "dnsmasq.service" {
		t.Errorf("dnsmasq 条目解析错误: %+v", ports[1])
	}
	if ports[2].Port != 1194 || ports[2].Proto != "udp" {
		t.Errorf("openvpn 条目解析错误: %+v", ports[2])
	}
	// IPv6 通配地址、无 users 段的行也应能解析
	if ports[4].Port != 8686 || ports[4].Comm != "ovpn-web" {
		t.Errorf("通配地址条目解析错误: %+v", ports[4])
	}
	if ports[5].Port != 631 || ports[5].PID != 0 {
		t.Errorf("无 users 段条目解析错误: %+v", ports[5])
	}
}
