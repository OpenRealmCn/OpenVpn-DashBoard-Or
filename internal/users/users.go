// Package users 管理子用户:细粒度权限、证书配额、启用/禁用。
// 管理员账号(admin)仍存于面板配置,不在本存储内。
package users

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"openvpntools/internal/store"
)

// Perms 与前端菜单/按钮一一对应,管理员隐含全部权限。
type Perms struct {
	View       bool `json:"view"`       // 查看仪表盘/证书列表/在线列表
	CertCreate bool `json:"certCreate"` // 创建证书 + 下载/二维码分享(仅自己创建的)
	CertRevoke bool `json:"certRevoke"` // 吊销(仅自己创建的)
	Install    bool `json:"install"`    // 安装向导与回滚
	Kick       bool `json:"kick"`       // 断开在线客户端
	Maintain   bool `json:"maintain"`   // 服务启停/版本升级/DNS Stub/ip_forward
}

// NodePerms 子节点上的细分操作权限。
type NodePerms struct {
	View         bool `json:"view"`         // 查看状态、在线客户端与证书列表
	CertCreate   bool `json:"certCreate"`   // 创建证书与下载配置
	CertRevoke   bool `json:"certRevoke"`   // 吊销证书
	Install      bool `json:"install"`      // 远程安装与预检
	Rollback     bool `json:"rollback"`     // 安装失败回滚
	Service      bool `json:"service"`      // OpenVPN 服务启停与重启
	Kick         bool `json:"kick"`         // 断开在线客户端
	Upgrade      bool `json:"upgrade"`      // 组件升级与系统维护(DNS/转发等)
	PanelRestart bool `json:"panelRestart"` // 重启子节点面板进程
}

// NodeGrant 一条节点授权;Full 即「完整管理」模板:忽略 Perms,全部放行。
type NodeGrant struct {
	NodeID string    `json:"nodeId"`
	Full   bool      `json:"full"`
	Perms  NodePerms `json:"perms"`
}

type User struct {
	Username   string      `json:"username"`
	Hash       string      `json:"hash"` // bcrypt,仅存储文件用,API 层不得外传
	Perms      Perms       `json:"perms"`
	CertLimit  int         `json:"certLimit"`         // 有效证书数上限,0 = 不限
	NodeIDs    []string    `json:"nodeIds,omitempty"` // 旧版字段:加载时迁移为完整管理授权
	NodeGrants []NodeGrant `json:"nodeGrants"`        // 节点授权(细分权限或完整管理)
	Disabled   bool        `json:"disabled"`
	CreatedAt  time.Time   `json:"createdAt"`
}

// normalize 旧数据迁移:nodeIds 等同「完整管理」授权。
func (u *User) normalize() {
	if len(u.NodeGrants) == 0 && len(u.NodeIDs) > 0 {
		for _, id := range u.NodeIDs {
			u.NodeGrants = append(u.NodeGrants, NodeGrant{NodeID: id, Full: true})
		}
	}
	u.NodeIDs = nil
}

var (
	usernameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)
	ErrNotFound   = errors.New("用户不存在")
	ErrExists     = errors.New("用户名已存在")
	ErrBadName    = errors.New("用户名需为 3-32 位小写字母/数字/下划线/连字符")
	ErrReserved   = errors.New("该用户名是保留名称")
	ErrWeakPass   = errors.New("密码长度至少 8 位")
	ErrDisabled   = errors.New("账号已被禁用")
	ErrBadCred    = errors.New("用户名或密码错误")
)

var reserved = map[string]bool{"admin": true, "root": true, "system": true}

type Store struct {
	mu    sync.Mutex
	path  string
	users map[string]*User
}

func Load(path string) (*Store, error) {
	s := &Store{path: path, users: map[string]*User{}}
	var list []*User
	if err := store.LoadJSON(path, &list); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("加载用户文件失败: %w", err)
		}
	}
	for _, u := range list {
		u.normalize()
		s.users[u.Username] = u
	}
	return s, nil
}

// Reload 从磁盘重新加载(备份恢复后调用)。
func (s *Store) Reload() error {
	var list []*User
	if err := store.LoadJSON(s.path, &list); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = map[string]*User{}
	for _, u := range list {
		u.normalize()
		s.users[u.Username] = u
	}
	return nil
}

func (s *Store) saveLocked() error {
	list := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	return store.SaveJSON(s.path, list)
}

// List 返回副本(含 Hash,调用方负责脱敏)。
func (s *Store) List() []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) Get(username string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return User{}, ErrNotFound
	}
	return *u, nil
}

func (s *Store) Create(username, password string, perms Perms, certLimit int, grants []NodeGrant) error {
	if !usernameRe.MatchString(username) {
		return ErrBadName
	}
	if reserved[username] {
		return ErrReserved
	}
	if len(password) < 8 {
		return ErrWeakPass
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[username]; ok {
		return ErrExists
	}
	s.users[username] = &User{
		Username: username, Hash: string(hash), Perms: perms,
		CertLimit: max(certLimit, 0), NodeGrants: grants, CreatedAt: time.Now(),
	}
	return s.saveLocked()
}

// Update 原子修改;fn 内可改 Perms/CertLimit/Disabled/Hash。
func (s *Store) Update(username string, fn func(*User) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return ErrNotFound
	}
	next := *u
	if err := fn(&next); err != nil {
		return err
	}
	next.Username = u.Username // 用户名不可改
	s.users[username] = &next
	return s.saveLocked()
}

func (s *Store) SetPassword(username, password string) error {
	if len(password) < 8 {
		return ErrWeakPass
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	return s.Update(username, func(u *User) error {
		u.Hash = string(hash)
		return nil
	})
}

func (s *Store) Delete(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[username]; !ok {
		return ErrNotFound
	}
	delete(s.users, username)
	return s.saveLocked()
}

// Verify 校验密码并检查禁用状态。
func (s *Store) Verify(username, password string) (User, error) {
	s.mu.Lock()
	u, ok := s.users[username]
	if !ok {
		s.mu.Unlock()
		return User{}, ErrBadCred
	}
	cp := *u
	s.mu.Unlock()
	if bcrypt.CompareHashAndPassword([]byte(cp.Hash), []byte(password)) != nil {
		return User{}, ErrBadCred
	}
	if cp.Disabled {
		return User{}, ErrDisabled
	}
	return cp, nil
}
