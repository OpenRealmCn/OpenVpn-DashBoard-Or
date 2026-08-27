package installer

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"

	"openvpntools/internal/dnsguard"
	"openvpntools/internal/platform/mock"
)

func TestJournalWriteAheadAndReload(t *testing.T) {
	dir := t.TempDir()
	path := JournalPath(dir)

	j, err := OpenJournal(path)
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}
	if err := j.Record("s1", ActFileCreated, FilePayload{Path: "/tmp/a"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := j.Record("s2", ActDirCreated, FilePayload{Path: "/tmp/dir"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 模拟进程重启后加载残留 journal
	entries := LoadJournalEntries(path)
	if len(entries) != 2 {
		t.Fatalf("期望 2 条,实际 %d", len(entries))
	}
	if entries[0].Seq != 1 || entries[0].StepID != "s1" || entries[0].Action != ActFileCreated {
		t.Errorf("第一条内容不对: %+v", entries[0])
	}

	// 追加续写时序号应接上
	j2, err := OpenJournal(path)
	if err != nil {
		t.Fatalf("重新打开: %v", err)
	}
	if err := j2.Record("s3", ActFileCreated, FilePayload{Path: "/tmp/b"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got := j2.Entries(); got[len(got)-1].Seq != 3 {
		t.Errorf("重启后序号未接上: %+v", got)
	}
	_ = j2.CloseAndRemove()
	if len(LoadJournalEntries(path)) != 0 {
		t.Errorf("CloseAndRemove 后 journal 应不存在")
	}
}

func TestRollbackFileActions(t *testing.T) {
	dataDir := t.TempDir()
	plat := mock.New(dataDir)
	fs := plat.FS()
	j, err := OpenJournal(JournalPath(dataDir))
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}

	// 场景 1:新建文件 → 回滚后删除
	created := filepath.Join(dataDir, "mockroot", "created.conf")
	if err := j.Record("s", ActFileCreated, FilePayload{Path: created}); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFileAtomic(created, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 场景 2:覆盖已有文件 → 回滚后恢复旧内容
	replaced := filepath.Join(dataDir, "mockroot", "replaced.conf")
	if err := fs.WriteFileAtomic(replaced, []byte("old-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := j.Record("s", ActFileReplaced, FileReplacedPayload{
		Path: replaced, OldB64: base64.StdEncoding.EncodeToString([]byte("old-content")), Perm: 0o644,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFileAtomic(replaced, []byte("new-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 场景 3:新建目录 → 回滚后整体删除
	dir := filepath.Join(dataDir, "mockroot", "easy-rsa")
	if err := j.Record("s", ActDirCreated, FilePayload{Path: dir}); err != nil {
		t.Fatal(err)
	}
	if err := fs.MkdirAll(filepath.Join(dir, "pki"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = j.Close()

	rb := &Rollbacker{Plat: plat, DNS: dnsguard.New(plat), Log: func(string, ...any) {}}
	failures := rb.Rollback(context.Background(), j.Entries())
	if len(failures) != 0 {
		t.Fatalf("回滚出现失败项: %v", failures)
	}
	if fs.Exists(created) {
		t.Error("file_created 未被删除")
	}
	if data, _ := fs.ReadFile(replaced); string(data) != "old-content" {
		t.Errorf("file_replaced 未恢复旧内容,实际 %q", data)
	}
	if fs.Exists(dir) {
		t.Error("dir_created 未被删除")
	}
}

func TestParamsNormalize(t *testing.T) {
	p := Params{PublicAddr: "vpn.example.com"}
	if err := p.Normalize(); err != nil {
		t.Fatalf("默认参数应通过: %v", err)
	}
	if p.Port != 1194 || p.Proto != "udp" || p.Subnet != "10.8.0.0/24" || p.DNSMode != DNSCloudflare {
		t.Errorf("默认值不对: %+v", p)
	}
	if p.GatewayIP() != "10.8.0.1" {
		t.Errorf("GatewayIP = %s", p.GatewayIP())
	}
	net, mask := p.Network()
	if net != "10.8.0.0" || mask != "255.255.255.0" {
		t.Errorf("Network = %s %s", net, mask)
	}

	bad := []Params{
		{PublicAddr: "x", Port: 70000},
		{PublicAddr: "x", Proto: "sctp"},
		{PublicAddr: "x", Subnet: "10.8.0.1/24"},
		{PublicAddr: "x", Subnet: "fd00::/64"},
		{PublicAddr: ""},
		{PublicAddr: "x", DNSMode: DNSCustom, DNS1: "not-an-ip"},
		{PublicAddr: "bad addr with spaces"},
	}
	for i, b := range bad {
		if err := b.Normalize(); err == nil {
			t.Errorf("用例 %d 应报错: %+v", i, b)
		}
	}
}
