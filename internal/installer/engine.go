package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"openvpntools/internal/config"
	"openvpntools/internal/dnsguard"
	"openvpntools/internal/platform"
)

type JobState string

const (
	StateRunning      JobState = "running"
	StateSuccess      JobState = "success"
	StateRolledBack   JobState = "rolled_back"     // 失败但已完整回滚
	StateRollbackFail JobState = "rollback_failed" // 失败且部分未复原
)

type StepInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // pending / running / done / failed / skipped
}

type Job struct {
	mu         sync.Mutex
	ID         string
	Params     Params
	State      JobState
	Error      string
	Steps      []StepInfo
	StartedAt  time.Time
	FinishedAt time.Time
	Logs       *LogBuffer
	done       chan struct{}
}

func (j *Job) Done() <-chan struct{} { return j.done }

type JobSnapshot struct {
	ID         string     `json:"id"`
	State      JobState   `json:"state"`
	Error      string     `json:"error,omitempty"`
	Steps      []StepInfo `json:"steps"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt time.Time  `json:"finishedAt,omitempty"`
}

func (j *Job) Snapshot() JobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	steps := make([]StepInfo, len(j.Steps))
	copy(steps, j.Steps)
	return JobSnapshot{
		ID: j.ID, State: j.State, Error: j.Error, Steps: steps,
		StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
	}
}

func (j *Job) setStep(i int, status string) {
	j.mu.Lock()
	j.Steps[i].Status = status
	j.mu.Unlock()
}

func (j *Job) finish(state JobState, errMsg string) {
	j.mu.Lock()
	j.State, j.Error, j.FinishedAt = state, errMsg, time.Now()
	j.mu.Unlock()
}

// Engine 串行调度安装任务(同一时间只允许一个)。
type Engine struct {
	plat     platform.Platform
	dns      *dnsguard.Guard
	cfg      *config.Manager
	simulate bool

	mu  sync.Mutex
	job *Job
}

func NewEngine(plat platform.Platform, dns *dnsguard.Guard, cfg *config.Manager, simulate bool) *Engine {
	return &Engine{plat: plat, dns: dns, cfg: cfg, simulate: simulate}
}

func (e *Engine) Simulate() bool { return e.simulate }

func (e *Engine) Installed() bool {
	return e.plat.FS().Exists(filepath.Join(e.plat.Paths().ServerConfDir, "server.conf"))
}

func (e *Engine) journalPath() string { return JournalPath(e.plat.Paths().DataDir) }

// PendingJournal 表示存在失败残留(需先回滚才能重新安装)。
func (e *Engine) PendingJournal() bool {
	if e.CurrentJob() != nil && e.CurrentJob().Snapshot().State == StateRunning {
		return false
	}
	return e.plat.FS().Exists(e.journalPath())
}

func (e *Engine) CurrentJob() *Job {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.job
}

func (e *Engine) Precheck(ctx context.Context, p Params) (PrecheckReport, error) {
	if p.PublicAddr == "" {
		p.PublicAddr = "pending.example" // 预检阶段允许未填公网地址
	}
	if err := p.Normalize(); err != nil {
		return PrecheckReport{}, err
	}
	return RunPrecheck(ctx, e.plat, e.dns, p, e.simulate), nil
}

func (e *Engine) Start(p Params) (string, error) {
	if err := p.Normalize(); err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.job != nil && e.job.Snapshot().State == StateRunning {
		return "", errors.New("已有安装任务进行中")
	}
	if e.Installed() {
		return "", errors.New("已检测到 OpenVPN 配置(server.conf),仅支持全新安装")
	}
	if e.plat.FS().Exists(e.journalPath()) {
		return "", errors.New("存在上次失败的安装残留,请先执行回滚")
	}
	journal, err := OpenJournal(e.journalPath())
	if err != nil {
		return "", fmt.Errorf("创建回滚日志失败: %w", err)
	}
	steps := buildSteps()
	infos := make([]StepInfo, len(steps))
	for i, st := range steps {
		infos[i] = StepInfo{ID: st.ID, Name: st.Name, Status: "pending"}
	}
	job := &Job{
		ID:        fmt.Sprintf("job-%d", time.Now().UnixMilli()),
		Params:    p,
		State:     StateRunning,
		Steps:     infos,
		StartedAt: time.Now(),
		Logs:      NewLogBuffer(),
		done:      make(chan struct{}),
	}
	e.job = job
	go e.run(job, journal, steps)
	return job.ID, nil
}

func (e *Engine) run(job *Job, journal *Journal, steps []Step) {
	defer close(job.done)
	ctx := context.Background()
	sc := &StepCtx{
		Ctx: ctx, Plat: e.plat, DNS: e.dns, Journal: journal,
		Params: job.Params, Data: &SharedData{},
		Mirror: e.cfg.Snapshot().GithubMirror, Simulate: e.simulate,
	}
	job.Logs.Append("state", "", fmt.Sprintf("开始安装(端口 %s/%d,DNS 模式 %s)",
		job.Params.Proto, job.Params.Port, job.Params.DNSMode))

	for i := range steps {
		st := steps[i]
		if st.Skip != nil && st.Skip(sc) {
			job.setStep(i, "skipped")
			job.Logs.Append("info", st.ID, "跳过: "+st.Name)
			continue
		}
		job.setStep(i, "running")
		sc.StepID = st.ID
		stepID := st.ID
		sc.Log = func(format string, args ...any) {
			job.Logs.Append("info", stepID, fmt.Sprintf(format, args...))
		}
		job.Logs.Append("step", st.ID, "▶ "+st.Name)
		if err := st.Run(sc); err != nil {
			job.setStep(i, "failed")
			job.Logs.Append("error", st.ID, err.Error())
			e.failAndRollback(ctx, job, journal, err)
			return
		}
		job.setStep(i, "done")
	}
	if err := journal.CloseAndRemove(); err != nil {
		job.Logs.Append("info", "", "清理回滚日志失败(不影响安装结果): "+err.Error())
	}
	if err := SaveParams(e.plat.Paths().DataDir, job.Params); err != nil {
		job.Logs.Append("info", "", "保存安装参数失败(客户端配置将无法生成): "+err.Error())
	}
	job.finish(StateSuccess, "")
	job.Logs.Append("state", "", "✅ 安装完成,OpenVPN 已启动")
}

func (e *Engine) failAndRollback(ctx context.Context, job *Job, journal *Journal, cause error) {
	job.Logs.Append("state", "", "安装失败,开始按回滚日志逆序撤销 …")
	_ = journal.Close()
	rb := &Rollbacker{
		Plat: e.plat, DNS: e.dns,
		Log: func(format string, args ...any) {
			job.Logs.Append("info", "rollback", fmt.Sprintf(format, args...))
		},
	}
	failures := rb.Rollback(ctx, journal.Entries())
	if len(failures) == 0 {
		_ = os.Remove(e.journalPath())
		job.finish(StateRolledBack, cause.Error())
		job.Logs.Append("state", "", "↩ 已完整回滚,系统恢复原状")
		return
	}
	job.finish(StateRollbackFail, cause.Error()+";未复原: "+strings.Join(failures, "; "))
	job.Logs.Append("state", "", fmt.Sprintf("⚠ 回滚完成但有 %d 项未复原,journal 已保留,可修复后重试回滚", len(failures)))
}

// RollbackPending 手动回滚磁盘上的失败残留(含面板重启后的场景)。
func (e *Engine) RollbackPending(ctx context.Context) ([]string, []string, error) {
	e.mu.Lock()
	if e.job != nil && e.job.Snapshot().State == StateRunning {
		e.mu.Unlock()
		return nil, nil, errors.New("安装进行中,不能手动回滚")
	}
	e.mu.Unlock()

	entries := LoadJournalEntries(e.journalPath())
	if len(entries) == 0 {
		return nil, nil, errors.New("没有可回滚的安装残留")
	}
	var logs []string
	rb := &Rollbacker{
		Plat: e.plat, DNS: e.dns,
		Log: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	}
	failures := rb.Rollback(ctx, entries)
	if len(failures) == 0 {
		_ = os.Remove(e.journalPath())
	}
	return logs, failures, nil
}
