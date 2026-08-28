package nodes

// SSH 快捷安装:主节点凭用户提供的一次性 SSH 凭据登录目标机,
// 执行 install.sh join 完成面板安装、令牌生成与子节点自注册。
// 硬性规则:凭据只在本次任务内存中使用,绝不落盘、绝不写日志;
// 连接不校验主机指纹(UI 有明示,用户自行确认目标可信)。

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHInstallParams struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	AuthMethod string `json:"authMethod"` // password / key
	Password   string `json:"password"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
}

func (p *SSHInstallParams) normalize() error {
	p.Host = strings.TrimSpace(p.Host)
	if p.Host == "" || strings.ContainsAny(p.Host, " '\"`$\\") {
		return errors.New("主机地址不合法")
	}
	if p.Port == 0 {
		p.Port = 22
	}
	if p.Port < 1 || p.Port > 65535 {
		return errors.New("SSH 端口不合法")
	}
	p.User = strings.TrimSpace(p.User)
	if p.User == "" {
		p.User = "root"
	}
	switch p.AuthMethod {
	case "password":
		if p.Password == "" {
			return errors.New("请输入 SSH 密码")
		}
	case "key":
		if strings.TrimSpace(p.PrivateKey) == "" {
			return errors.New("请粘贴 SSH 私钥")
		}
	default:
		return errors.New("认证方式必须是 password 或 key")
	}
	return nil
}

// JoinTarget 是绑定到主节点所需的信息(由 API 层填充)。
type JoinTarget struct {
	PanelURL  string // 子节点回连本面板的地址
	Code      string // 一次性绑定码
	ScriptURL string // install.sh 地址
	Mirror    string // GH_MIRROR,可空
}

type SSHJobSnapshot struct {
	ID        string    `json:"id"`
	Host      string    `json:"host"`
	State     string    `json:"state"` // running / success / failed
	Error     string    `json:"error,omitempty"`
	Logs      []string  `json:"logs"`
	StartedAt time.Time `json:"startedAt"`
}

type SSHJob struct {
	mu        sync.Mutex
	id, host  string
	state     string
	errMsg    string
	logs      []string
	startedAt time.Time
	doneAt    time.Time
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func (j *SSHJob) logf(format string, args ...any) {
	line := ansiRe.ReplaceAllString(fmt.Sprintf(format, args...), "")
	j.mu.Lock()
	j.logs = append(j.logs, line)
	j.mu.Unlock()
}

func (j *SSHJob) finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.doneAt = time.Now()
	if err != nil {
		j.state, j.errMsg = "failed", err.Error()
		return
	}
	j.state = "success"
}

func (j *SSHJob) Snapshot() SSHJobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	logs := make([]string, len(j.logs))
	copy(logs, j.logs)
	return SSHJobSnapshot{
		ID: j.id, Host: j.host, State: j.state, Error: j.errMsg,
		Logs: logs, StartedAt: j.startedAt,
	}
}

// SSHJobs 是内存态任务表;完成 1 小时后的任务在新任务启动时清理。
type SSHJobs struct {
	mu    sync.Mutex
	items map[string]*SSHJob
}

func NewSSHJobs() *SSHJobs { return &SSHJobs{items: map[string]*SSHJob{}} }

func (s *SSHJobs) Get(id string) (*SSHJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.items[id]
	return j, ok
}

func (s *SSHJobs) Start(p SSHInstallParams, t JoinTarget, simulate bool) (SSHJobSnapshot, error) {
	if err := p.normalize(); err != nil {
		return SSHJobSnapshot{}, err
	}
	auths, err := buildAuth(p)
	if err != nil {
		return SSHJobSnapshot{}, err
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return SSHJobSnapshot{}, err
	}
	job := &SSHJob{id: hex.EncodeToString(buf), host: p.Host, state: "running", startedAt: time.Now()}

	s.mu.Lock()
	for id, old := range s.items {
		old.mu.Lock()
		stale := old.state != "running" && time.Since(old.doneAt) > time.Hour
		old.mu.Unlock()
		if stale {
			delete(s.items, id)
		}
	}
	s.items[job.id] = job
	s.mu.Unlock()

	if simulate {
		go runSimulated(job, p, t)
	} else {
		go runSSHInstall(job, p, auths, t)
	}
	return job.Snapshot(), nil
}

// buildAuth 预先解析凭据,格式错误立即反馈而不是留到任务里。
func buildAuth(p SSHInstallParams) ([]ssh.AuthMethod, error) {
	if p.AuthMethod == "key" {
		var signer ssh.Signer
		var err error
		if p.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(p.PrivateKey), []byte(p.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(p.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("私钥解析失败(如有密码请填写 passphrase): %v", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	pw := p.Password
	return []ssh.AuthMethod{
		ssh.Password(pw),
		// 部分 sshd 只开 keyboard-interactive,用同一密码应答
		ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = pw
			}
			return answers, nil
		}),
	}, nil
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// buildJoinCmd 组装远程命令:下载 install.sh 到临时文件再以 root 执行 join。
// 不用 curl|bash 管道是因为 sudo -S 需要独占 stdin 读密码。
func buildJoinCmd(p SSHInstallParams, t JoinTarget) string {
	runner := "bash"
	if t.Mirror != "" {
		runner = "env GH_MIRROR=" + shq(t.Mirror) + " bash"
	}
	prefix := ""
	if p.User != "root" {
		if p.AuthMethod == "password" {
			prefix = "sudo -H -S -p '' " // 密码经 stdin 提供
		} else {
			prefix = "sudo -H -n " // 密钥登录要求免密 sudo
		}
	}
	return fmt.Sprintf(
		`t=$(mktemp) || exit 1; curl -fsSL %s -o "$t" || { echo "下载 install.sh 失败(目标机需已安装 curl 且能访问 GitHub 或镜像)" >&2; rm -f "$t"; exit 1; }; %s%s "$t" join %s %s; rc=$?; rm -f "$t"; exit $rc`,
		shq(t.ScriptURL), prefix, runner, shq(t.PanelURL), shq(t.Code))
}

func runSSHInstall(job *SSHJob, p SSHInstallParams, auths []ssh.AuthMethod, t JoinTarget) {
	addr := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	job.logf("连接 %s(用户 %s)…", addr, p.User)
	if p.User != "root" && p.AuthMethod == "key" {
		job.logf("提示: 非 root 密钥登录要求目标机 sudo 免密;否则请改用密码认证或 root 用户")
	}
	cfg := &ssh.ClientConfig{
		User:            p.User,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec — 快捷安装不校验主机指纹,UI 有明示
		Timeout:         15 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		job.finish(fmt.Errorf("SSH 连接失败: %v", err))
		return
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		job.finish(fmt.Errorf("创建 SSH 会话失败: %v", err))
		return
	}
	defer session.Close()

	if p.User != "root" && p.AuthMethod == "password" {
		session.Stdin = strings.NewReader(p.Password + "\n")
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		job.finish(err)
		return
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		job.finish(err)
		return
	}
	var wg sync.WaitGroup
	stream := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 256*1024)
		for sc.Scan() {
			if line := strings.TrimRight(sc.Text(), "\r"); strings.TrimSpace(line) != "" {
				job.logf("%s", line)
			}
		}
	}
	wg.Add(2)
	go stream(stdout)
	go stream(stderr)

	job.logf("已连接,开始远程安装(下载面板 → 生成令牌 → 注册到主节点)…")
	if err := session.Start(buildJoinCmd(p, t)); err != nil {
		job.finish(fmt.Errorf("启动远程命令失败: %v", err))
		return
	}
	done := make(chan error, 1)
	go func() {
		wg.Wait()
		done <- session.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			job.finish(errors.New("远程安装失败,详见上方日志"))
			return
		}
		job.logf("✅ 安装与绑定完成,节点将自动出现在列表中")
		job.finish(nil)
	case <-time.After(15 * time.Minute):
		client.Close()
		job.finish(errors.New("远程安装超时(15 分钟),请登录目标机检查"))
	}
}

// runSimulated 供 mock 演示模式:不建立任何网络连接,输出示意日志。
func runSimulated(job *SSHJob, p SSHInstallParams, t JoinTarget) {
	lines := []string{
		fmt.Sprintf("(mock) 连接 %s:%d(用户 %s)…", p.Host, p.Port, p.User),
		"(mock) 下载 install.sh …",
		"(mock) [ovpn-web] 下载 ovpn-web-linux-amd64(latest)…",
		"(mock) [ovpn-web] SHA256 校验通过",
		"(mock) [ovpn-web] 生成本节点接入令牌(写入面板配置)…",
		fmt.Sprintf("(mock) [ovpn-web] 向主节点注册: %s …", t.PanelURL),
		"(mock) [ovpn-web] 绑定成功!已成为主节点的子节点",
	}
	for _, l := range lines {
		time.Sleep(600 * time.Millisecond)
		job.logf("%s", l)
	}
	job.logf("✅ (mock)演示完成:真实环境中此时节点已出现在列表中")
	job.finish(nil)
}
