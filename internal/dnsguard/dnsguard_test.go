package dnsguard

import (
	"testing"

	"openvpntools/internal/platform"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		in   platform.PortInfo
		want Class
	}{
		{"resolved 按 unit 识别", platform.PortInfo{Comm: "whatever", Unit: "systemd-resolved.service"}, ClassResolved},
		{"resolved 按进程名识别", platform.PortInfo{Comm: "systemd-resolve"}, ClassResolved},
		{"dnsmasq 是已知 DNS", platform.PortInfo{Comm: "dnsmasq", Unit: "dnsmasq.service"}, ClassKnownDNS},
		{"unbound 是已知 DNS", platform.PortInfo{Comm: "unbound"}, ClassKnownDNS},
		{"bind named 是已知 DNS", platform.PortInfo{Comm: "named"}, ClassKnownDNS},
		{"未知进程绝不归入可处理类", platform.PortInfo{Comm: "weird-daemon", Unit: "weird.service"}, ClassUnknown},
		{"空信息视为未知", platform.PortInfo{}, ClassUnknown},
	}
	for _, tc := range cases {
		if got := Classify(tc.in); got != tc.want {
			t.Errorf("%s: Classify(%+v) = %s, 期望 %s", tc.name, tc.in, got, tc.want)
		}
	}
}
