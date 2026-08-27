package easyrsa

import (
	"testing"
	"time"
)

const indexFixture = "V\t350824120000Z\t\t01\tunknown\t/CN=server\n" +
	"V\t350824120000Z\t\t02\tunknown\t/CN=alice\n" +
	"R\t350824120000Z\t250810093000Z\t03\tunknown\t/CN=bob\n" +
	"R\t350824120000Z\t250810093000Z,keyCompromise\t04\tunknown\t/CN=carol\n" +
	"V\t200101000000Z\t\t05\tunknown\t/CN=old-expired\n" +
	"\n损坏的行\n"

func TestParseIndex(t *testing.T) {
	list := ParseIndex([]byte(indexFixture))
	if len(list) != 5 {
		t.Fatalf("期望 5 条,实际 %d: %+v", len(list), list)
	}
	byCN := map[string]CertInfo{}
	for _, c := range list {
		byCN[c.CN] = c
	}
	if byCN["alice"].Status != StatusValid || byCN["alice"].Serial != "02" {
		t.Errorf("alice 解析错误: %+v", byCN["alice"])
	}
	if byCN["bob"].Status != StatusRevoked || byCN["bob"].RevokedAt == nil {
		t.Errorf("bob 应为已吊销并带吊销时间: %+v", byCN["bob"])
	}
	if byCN["carol"].RevokedAt == nil {
		t.Errorf("carol 带吊销原因的时间未解析: %+v", byCN["carol"])
	}
	if byCN["old-expired"].Status != StatusExpired {
		t.Errorf("过期证书应标记为 E: %+v", byCN["old-expired"])
	}
	wantExp := time.Date(2035, 8, 24, 12, 0, 0, 0, time.UTC)
	if !byCN["alice"].Expiry.Equal(wantExp) {
		t.Errorf("到期时间解析错误: %v", byCN["alice"].Expiry)
	}
}
