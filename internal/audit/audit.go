// Package audit 持久化操作审计(JSONL):谁在何时对什么做了什么、结果如何。
// 只记录动作与路径,绝不记录请求体(密码等敏感字段)。
package audit

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Time   time.Time `json:"time"`
	User   string    `json:"user"` // "-" 表示未登录(如登录失败)
	Action string    `json:"action"`
	Status int       `json:"status"`
	IP     string    `json:"ip"`
}

const rotateSize = 1 << 20 // 1MB 轮转一次,保留一份旧档

type Logger struct {
	mu   sync.Mutex
	path string
}

func New(path string) *Logger { return &Logger{path: path} }

func (l *Logger) Append(e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if st, err := os.Stat(l.path); err == nil && st.Size() > rotateSize {
		_ = os.Remove(l.path + ".1")
		_ = os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return // 审计失败不阻塞业务
	}
	defer f.Close()
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
}

// Tail 返回最近 n 条,新的在前。
func (l *Logger) Tail(n int) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := os.ReadFile(l.path)
	if err != nil {
		return []Entry{}
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	out := make([]Entry, 0, n)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		var e Entry
		if json.Unmarshal([]byte(lines[i]), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}
