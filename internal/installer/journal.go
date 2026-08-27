// Package installer 实现分步安装引擎:write-ahead 回滚日志、
// 步骤调度、SSE 日志流与失败回滚。
package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type EntryAction string

const (
	ActFileCreated  EntryAction = "file_created"   // {path}
	ActFileReplaced EntryAction = "file_replaced"  // {path, oldB64, perm}
	ActDirCreated   EntryAction = "dir_created"    // {path}
	ActPkgInstalled EntryAction = "pkg_installed"  // {pkgs}
	ActSysctlSet    EntryAction = "sysctl_set"     // {file, key, oldRuntime, hadFile, oldFileB64}
	ActDNSStub      EntryAction = "dns_stub"       // dnsguard.StubBackup
	ActIptables     EntryAction = "iptables_rules" // {rules}
	ActServiceState EntryAction = "svc_state"      // {unit, wasActive, wasEnabled}
)

type JournalEntry struct {
	Seq     int             `json:"seq"`
	StepID  string          `json:"stepId"`
	Action  EntryAction     `json:"action"`
	Payload json.RawMessage `json:"payload"`
	At      time.Time       `json:"at"`
}

// Journal 是 write-ahead 回滚日志(JSONL):每条先 append+fsync 落盘,
// 对应动作才允许执行;失败时按 LIFO 逆序撤销。
type Journal struct {
	mu      sync.Mutex
	path    string
	f       *os.File
	entries []JournalEntry
}

func JournalPath(dataDir string) string {
	return filepath.Join(dataDir, "install-journal.jsonl")
}

func OpenJournal(path string) (*Journal, error) {
	j := &Journal{path: path}
	j.entries = readEntries(path) // 进程崩溃后残留的条目也要接上序号
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	j.f = f
	return j, nil
}

// LoadJournalEntries 只读加载(面板重启后对残留 journal 做回滚用)。
func LoadJournalEntries(path string) []JournalEntry { return readEntries(path) }

func readEntries(path string) []JournalEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []JournalEntry
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var e JournalEntry
		if json.Unmarshal([]byte(ln), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

// Record 先落盘(fsync)再返回;返回后调用方才能执行对应动作。
func (j *Journal) Record(stepID string, action EntryAction, payload any) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("journal 序列化失败: %w", err)
	}
	e := JournalEntry{
		Seq: len(j.entries) + 1, StepID: stepID, Action: action, Payload: raw, At: time.Now(),
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := j.f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("journal 写入失败: %w", err)
	}
	if err := j.f.Sync(); err != nil {
		return fmt.Errorf("journal 刷盘失败: %w", err)
	}
	j.entries = append(j.entries, e)
	return nil
}

func (j *Journal) Entries() []JournalEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]JournalEntry, len(j.entries))
	copy(out, j.entries)
	return out
}

// CloseAndRemove 安装成功后删除 journal。
func (j *Journal) CloseAndRemove() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f != nil {
		j.f.Close()
		j.f = nil
	}
	return os.Remove(j.path)
}

// Close 安装失败后保留 journal 文件,供(再次)回滚。
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f != nil {
		err := j.f.Close()
		j.f = nil
		return err
	}
	return nil
}
