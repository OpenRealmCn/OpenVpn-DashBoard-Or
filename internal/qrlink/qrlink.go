// Package qrlink 管理一次性下载 token:crypto/rand 高熵、TTL、
// 严格单次消费(compare-and-delete);内存态,面板重启即全部失效。
package qrlink

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const DefaultTTL = 10 * time.Minute

type item struct {
	cn      string
	expires time.Time
}

type Store struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]item
}

func New(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Store{ttl: ttl, items: map[string]item{}}
}

// Create 为 cn 生成一次性 token;同一 cn 的旧 token 立即作废。
func (s *Store) Create(cn string) (string, time.Time) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand 不可用: " + err.Error())
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	expires := time.Now().Add(s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	for t, it := range s.items {
		if it.cn == cn || time.Now().After(it.expires) {
			delete(s.items, t)
		}
	}
	s.items[token] = item{cn: cn, expires: expires}
	return token, expires
}

// Consume 单次消费:命中即删除;过期或已用返回 false。
func (s *Store) Consume(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[token]
	if !ok {
		return "", false
	}
	delete(s.items, token)
	if time.Now().After(it.expires) {
		return "", false
	}
	return it.cn, true
}
