package installer

import (
	"sync"
	"time"
)

type LogEvent struct {
	Seq   int       `json:"seq"`
	Time  time.Time `json:"time"`
	Level string    `json:"level"` // info / error / step / state
	Step  string    `json:"step,omitempty"`
	Msg   string    `json:"msg"`
}

// LogBuffer 保存安装日志并支持 SSE 订阅;Seq 单调递增,
// 断线重连按 Last-Event-ID 从 Snapshot 补发。
type LogBuffer struct {
	mu   sync.Mutex
	evs  []LogEvent
	subs map[chan LogEvent]struct{}
}

func NewLogBuffer() *LogBuffer {
	return &LogBuffer{subs: map[chan LogEvent]struct{}{}}
}

func (b *LogBuffer) Append(level, step, msg string) {
	b.mu.Lock()
	e := LogEvent{Seq: len(b.evs) + 1, Time: time.Now(), Level: level, Step: step, Msg: msg}
	b.evs = append(b.evs, e)
	for ch := range b.subs {
		select {
		case ch <- e:
		default: // 订阅方消费太慢就丢给它自己按 Seq 补
		}
	}
	b.mu.Unlock()
}

// Snapshot 返回 seq > afterSeq 的历史事件。
func (b *LogBuffer) Snapshot(afterSeq int) []LogEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	if afterSeq < 0 {
		afterSeq = 0
	}
	if afterSeq >= len(b.evs) {
		return nil
	}
	out := make([]LogEvent, len(b.evs)-afterSeq)
	copy(out, b.evs[afterSeq:])
	return out
}

func (b *LogBuffer) Subscribe() (<-chan LogEvent, func()) {
	ch := make(chan LogEvent, 512)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
	return ch, cancel
}
