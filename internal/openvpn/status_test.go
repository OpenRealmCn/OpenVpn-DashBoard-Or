package openvpn

import "testing"

const statusFixture = `OpenVPN CLIENT LIST
Updated,Thu Aug 27 12:00:00 2026
Common Name,Real Address,Bytes Received,Bytes Sent,Connected Since
alice,1.2.3.4:55555,123456,789012,Thu Aug 27 11:00:00 2026
bob,5.6.7.8:44444,1000,2000,Thu Aug 27 10:30:00 2026
ROUTING TABLE
Virtual Address,Common Name,Real Address,Last Ref
10.8.0.2,alice,1.2.3.4:55555,Thu Aug 27 11:59:00 2026
10.8.0.3,bob,5.6.7.8:44444,Thu Aug 27 11:58:00 2026
GLOBAL STATS
Max bcast/mcast queue length,1
END
`

func TestParseStatusLog(t *testing.T) {
	list := ParseStatusLog([]byte(statusFixture))
	if len(list) != 2 {
		t.Fatalf("期望 2 个在线客户端,实际 %d: %+v", len(list), list)
	}
	a := list[0]
	if a.CN != "alice" || a.RealAddr != "1.2.3.4:55555" ||
		a.BytesRecv != 123456 || a.BytesSent != 789012 || a.VirtualAddr != "10.8.0.2" {
		t.Errorf("alice 解析错误: %+v", a)
	}
	if a.Since.IsZero() || a.Since.Hour() != 11 {
		t.Errorf("连接时间解析错误: %v", a.Since)
	}
	if list[1].VirtualAddr != "10.8.0.3" {
		t.Errorf("bob 虚拟地址错误: %+v", list[1])
	}

	if got := ParseStatusLog([]byte("")); len(got) != 0 {
		t.Errorf("空文件应返回空列表")
	}
}
