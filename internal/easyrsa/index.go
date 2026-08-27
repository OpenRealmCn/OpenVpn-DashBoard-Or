package easyrsa

import (
	"strings"
	"time"
)

type CertStatus string

const (
	StatusValid   CertStatus = "V"
	StatusRevoked CertStatus = "R"
	StatusExpired CertStatus = "E"
)

type CertInfo struct {
	CN        string     `json:"cn"`
	Status    CertStatus `json:"status"`
	Expiry    time.Time  `json:"expiry"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	Serial    string     `json:"serial"`
}

// ParseIndex 解析 EasyRSA 的 pki/index.txt。
// 每行:status \t 到期时间 \t [吊销时间] \t 序列号 \t 文件名 \t DN(/CN=xxx)
func ParseIndex(data []byte) []CertInfo {
	var out []CertInfo
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 6 {
			continue
		}
		info := CertInfo{
			Status: CertStatus(strings.TrimSpace(f[0])),
			Serial: strings.TrimSpace(f[3]),
			CN:     dnCN(f[5]),
		}
		if t, err := parseASN1Time(strings.TrimSpace(f[1])); err == nil {
			info.Expiry = t
		}
		if rev := strings.TrimSpace(f[2]); rev != "" {
			// 吊销字段可能带原因,形如 250101120000Z,keyCompromise
			revTime := strings.SplitN(rev, ",", 2)[0]
			if t, err := parseASN1Time(revTime); err == nil {
				info.RevokedAt = &t
			}
		}
		// 有效但已过期的按 E 展示
		if info.Status == StatusValid && !info.Expiry.IsZero() && time.Now().After(info.Expiry) {
			info.Status = StatusExpired
		}
		if info.CN != "" {
			out = append(out, info)
		}
	}
	return out
}

func dnCN(dn string) string {
	for _, part := range strings.Split(dn, "/") {
		if v, ok := strings.CutPrefix(part, "CN="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseASN1Time 支持 UTCTime(YYMMDDHHMMSSZ)与 GeneralizedTime(YYYYMMDDHHMMSSZ)。
func parseASN1Time(s string) (time.Time, error) {
	if len(s) == 15 {
		return time.Parse("20060102150405Z", s)
	}
	return time.Parse("060102150405Z", s)
}
