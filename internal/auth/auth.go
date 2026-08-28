// Package auth 负责登录鉴权:管理员(admin,存于面板配置)与
// 子用户(users 存储)统一签发 JWT;权限在每次请求时实时解析,
// 修改权限或禁用账号立即生效。
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"openvpntools/internal/config"
	"openvpntools/internal/users"
)

const (
	CookieName    = "ovpn_session"
	AdminUsername = "admin"
	tokenTTL      = 24 * time.Hour
	bcryptCost    = 12
)

var (
	ErrAlreadySetup   = errors.New("管理员密码已设置")
	ErrNotInitialized = errors.New("面板尚未初始化,请先设置管理员密码")
	ErrWeakPassword   = errors.New("密码长度至少 8 位")
	ErrBadCredential  = errors.New("用户名或密码错误")
)

// Identity 是请求期身份;管理员隐含全部权限。
type Identity struct {
	Username  string      `json:"username"`
	IsAdmin   bool        `json:"isAdmin"`
	Perms     users.Perms `json:"perms"`
	CertLimit int         `json:"certLimit"` // 0 = 不限
	NodeIDs   []string    `json:"nodeIds"`   // 子用户可管理的节点;管理员为全部
}

func adminIdentity() Identity {
	return Identity{
		Username: AdminUsername, IsAdmin: true,
		Perms: users.Perms{View: true, CertCreate: true, CertRevoke: true, Install: true, Kick: true, Maintain: true},
	}
}

// MasterIdentity 是主节点经 node_token 接管本面板时的身份(等同管理员)。
func MasterIdentity() Identity {
	id := adminIdentity()
	id.Username = "node-master"
	return id
}

type claims struct {
	Admin bool `json:"adm"`
	jwt.RegisteredClaims
}

type Service struct {
	cfg   *config.Manager
	users *users.Store
}

func New(cfg *config.Manager, us *users.Store) *Service {
	return &Service{cfg: cfg, users: us}
}

func (s *Service) Initialized() bool { return s.cfg.Snapshot().AdminHash != "" }

// Setup 首次设置管理员密码。
func (s *Service) Setup(password string) error {
	if len(password) < 8 {
		return ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return err
	}
	return s.cfg.Update(func(c *config.Config) error {
		if c.AdminHash != "" {
			return ErrAlreadySetup
		}
		c.AdminHash = string(hash)
		return nil
	})
}

// Login 验证凭据并签发 token;username 为空时按管理员处理(兼容旧前端)。
func (s *Service) Login(username, password string) (string, time.Time, error) {
	if username == "" || username == AdminUsername {
		snap := s.cfg.Snapshot()
		if snap.AdminHash == "" {
			return "", time.Time{}, ErrNotInitialized
		}
		if bcrypt.CompareHashAndPassword([]byte(snap.AdminHash), []byte(password)) != nil {
			return "", time.Time{}, ErrBadCredential
		}
		return s.IssueToken(AdminUsername, true)
	}
	u, err := s.users.Verify(username, password)
	if err != nil {
		return "", time.Time{}, err
	}
	return s.IssueToken(u.Username, false)
}

func (s *Service) IssueToken(username string, admin bool) (string, time.Time, error) {
	exp := time.Now().Add(tokenTTL)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Admin: admin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	})
	signed, err := tok.SignedString([]byte(s.cfg.Snapshot().JWTSecret))
	return signed, exp, err
}

// Validate 校验 token 并实时解析身份(子用户被禁用/删除立即失效)。
func (s *Service) Validate(token string) (Identity, error) {
	secret := s.cfg.Snapshot().JWTSecret
	var cl claims
	_, err := jwt.ParseWithClaims(token, &cl,
		func(t *jwt.Token) (any, error) { return []byte(secret), nil },
		jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return Identity{}, err
	}
	if cl.Admin || cl.Subject == AdminUsername {
		return adminIdentity(), nil
	}
	u, err := s.users.Get(cl.Subject)
	if err != nil {
		return Identity{}, errors.New("用户不存在或已删除")
	}
	if u.Disabled {
		return Identity{}, users.ErrDisabled
	}
	return Identity{Username: u.Username, Perms: u.Perms, CertLimit: u.CertLimit, NodeIDs: u.NodeIDs}, nil
}

// ChangePassword 修改自己的密码(需验证旧密码)。
func (s *Service) ChangePassword(ident Identity, oldPass, newPass string) error {
	if len(newPass) < 8 {
		return ErrWeakPassword
	}
	if ident.IsAdmin {
		snap := s.cfg.Snapshot()
		if bcrypt.CompareHashAndPassword([]byte(snap.AdminHash), []byte(oldPass)) != nil {
			return ErrBadCredential
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcryptCost)
		if err != nil {
			return err
		}
		return s.cfg.Update(func(c *config.Config) error {
			c.AdminHash = string(hash)
			return nil
		})
	}
	if _, err := s.users.Verify(ident.Username, oldPass); err != nil {
		return ErrBadCredential
	}
	return s.users.SetPassword(ident.Username, newPass)
}
