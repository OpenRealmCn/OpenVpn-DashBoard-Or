// Package nodes 实现多节点管理(主节点侧):子节点注册表、
// 一次性绑定码、健康探测与请求转发。
// 子节点即普通面板实例,凭 node_token(Bearer)被主节点接管。
package nodes

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"openvpntools/internal/store"
)

type Node struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Token       string    `json:"token"` // 仅存于主节点数据目录(0600),API 层不外传
	InsecureTLS bool      `json:"insecureTLS"`
	AddedAt     time.Time `json:"addedAt"`
}

var (
	ErrNotFound = errors.New("节点不存在")
	ErrExists   = errors.New("同名或同地址的节点已存在")
)

type Store struct {
	mu    sync.Mutex
	path  string
	nodes []*Node
}

func Load(path string) (*Store, error) {
	s := &Store{path: path}
	if err := store.LoadJSON(path, &s.nodes); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("加载节点表失败: %w", err)
	}
	return s, nil
}

func (s *Store) saveLocked() error { return store.SaveJSON(s.path, s.nodes) }

// NormalizeURL 校验并规范化节点地址。
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("节点地址必须是完整的 http(s)://host:port")
	}
	return raw, nil
}

func (s *Store) List() []Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Node, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, *n)
	}
	return out
}

func (s *Store) Get(id string) (Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.nodes {
		if n.ID == id {
			return *n, nil
		}
	}
	return Node{}, ErrNotFound
}

func (s *Store) Add(name, rawURL, token string, insecure bool) (Node, error) {
	u, err := NormalizeURL(rawURL)
	if err != nil {
		return Node{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Node{}, errors.New("节点名称不能为空")
	}
	if token == "" {
		return Node{}, errors.New("节点令牌不能为空")
	}
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return Node{}, err
	}
	n := &Node{
		ID: hex.EncodeToString(buf), Name: name, URL: u,
		Token: token, InsecureTLS: insecure, AddedAt: time.Now(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ex := range s.nodes {
		if ex.Name == name || ex.URL == u {
			return Node{}, ErrExists
		}
	}
	s.nodes = append(s.nodes, n)
	return *n, s.saveLocked()
}

func (s *Store) Update(id string, fn func(*Node) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.nodes {
		if n.ID == id {
			next := *n
			if err := fn(&next); err != nil {
				return err
			}
			next.ID = n.ID
			*n = next
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, n := range s.nodes {
		if n.ID == id {
			s.nodes = append(s.nodes[:i], s.nodes[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// —— 一次性绑定码(内存态,默认 15 分钟)——

const joinCodeTTL = 15 * time.Minute

type JoinCodes struct {
	mu    sync.Mutex
	items map[string]time.Time
}

func NewJoinCodes() *JoinCodes { return &JoinCodes{items: map[string]time.Time{}} }

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 去掉易混淆字符

func (j *JoinCodes) Create() (string, time.Time) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand 不可用: " + err.Error())
	}
	chars := make([]byte, 12)
	for i, b := range buf {
		chars[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}
	code := fmt.Sprintf("%s-%s-%s", chars[0:4], chars[4:8], chars[8:12])
	exp := time.Now().Add(joinCodeTTL)
	j.mu.Lock()
	defer j.mu.Unlock()
	for c, e := range j.items {
		if time.Now().After(e) {
			delete(j.items, c)
		}
	}
	j.items[code] = exp
	return code, exp
}

// Consume 一次性消费绑定码。
func (j *JoinCodes) Consume(code string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	exp, ok := j.items[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return false
	}
	delete(j.items, strings.ToUpper(strings.TrimSpace(code)))
	return time.Now().Before(exp)
}
