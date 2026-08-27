package linux

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"openvpntools/internal/platform"
)

var ssUsersRe = regexp.MustCompile(`\(\("([^"]+)",pid=(\d+)`)

func (l *Linux) ListenPorts(ctx context.Context) ([]platform.PortInfo, error) {
	res, err := l.Run(ctx, platform.RunOpt{
		Argv: []string{"ss", "-H", "-tulnp"}, Timeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("ss -tulnp: %s", tail(res.Stderr, 400))
	}
	return ParseSS(res.Stdout, unitForPID), nil
}

// ParseSS 解析 `ss -H -tulnp` 输出;独立成函数便于用 fixture 做单测。
// unitFor 允许为 nil(不做 unit 反查)。
func ParseSS(out string, unitFor func(pid int) string) []platform.PortInfo {
	var ports []platform.PortInfo
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		proto := f[0]
		if proto != "tcp" && proto != "udp" {
			continue
		}
		local := f[4]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(local[i+1:])
		if err != nil {
			continue
		}
		p := platform.PortInfo{Proto: proto, Addr: local[:i], Port: port}
		if m := ssUsersRe.FindStringSubmatch(line); m != nil {
			p.Comm = m[1]
			p.PID, _ = strconv.Atoi(m[2])
			if unitFor != nil && p.PID > 0 {
				p.Unit = unitFor(p.PID)
			}
		}
		ports = append(ports, p)
	}
	return ports
}

// unitForPID 通过 /proc/<pid>/cgroup 反查进程所属 systemd unit,
// 形如 0::/system.slice/systemd-resolved.service → systemd-resolved.service。
func unitForPID(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if idx := strings.LastIndex(line, "/"); idx >= 0 {
			seg := strings.TrimSpace(line[idx+1:])
			if strings.HasSuffix(seg, ".service") {
				return seg
			}
		}
	}
	return ""
}
