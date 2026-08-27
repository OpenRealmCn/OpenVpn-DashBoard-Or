package openvpn

import (
	"strconv"
	"strings"
	"time"
)

type OnlineClient struct {
	CN          string    `json:"cn"`
	RealAddr    string    `json:"realAddr"`
	VirtualAddr string    `json:"virtualAddr"`
	BytesRecv   int64     `json:"bytesRecv"`
	BytesSent   int64     `json:"bytesSent"`
	Since       time.Time `json:"since"`
}

// ParseStatusLog 解析 status version 1 格式的状态文件(本工具生成的
// server.conf 使用默认 v1):CLIENT LIST 段取连接与流量,
// ROUTING TABLE 段补充虚拟地址。
func ParseStatusLog(data []byte) []OnlineClient {
	lines := strings.Split(string(data), "\n")
	section := ""
	var out []OnlineClient
	virtual := map[string]string{} // "cn|realAddr" -> 虚拟地址

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "OpenVPN CLIENT LIST"), strings.HasPrefix(line, "Common Name,Real Address"):
			section = "clients"
			continue
		case strings.HasPrefix(line, "ROUTING TABLE"), strings.HasPrefix(line, "Virtual Address,Common Name"):
			section = "routing"
			continue
		case strings.HasPrefix(line, "GLOBAL STATS"), strings.HasPrefix(line, "END"):
			section = ""
			continue
		case strings.HasPrefix(line, "Updated,"):
			continue
		}
		f := strings.Split(line, ",")
		switch section {
		case "clients":
			if len(f) < 5 || f[0] == "" {
				continue
			}
			c := OnlineClient{CN: f[0], RealAddr: f[1]}
			c.BytesRecv, _ = strconv.ParseInt(f[2], 10, 64)
			c.BytesSent, _ = strconv.ParseInt(f[3], 10, 64)
			if t, err := parseStatusTime(f[4]); err == nil {
				c.Since = t
			}
			out = append(out, c)
		case "routing":
			if len(f) >= 3 && f[0] != "" {
				virtual[f[1]+"|"+f[2]] = f[0]
			}
		}
	}
	for i := range out {
		out[i].VirtualAddr = virtual[out[i].CN+"|"+out[i].RealAddr]
	}
	return out
}

// parseStatusTime 解析 "Thu Aug 27 11:00:00 2026"(服务器本地时间)。
func parseStatusTime(s string) (time.Time, error) {
	return time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.TrimSpace(s), time.Local)
}
