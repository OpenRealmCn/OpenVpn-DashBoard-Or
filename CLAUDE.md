# CLAUDE.md

OpenVPN Web 管理面板(OpenVpn-DashBoard-Or):Go 单二进制内嵌 React SPA,面向 Debian 11+ / Ubuntu 20.04+。所有代码注释、UI 文案、提交信息均为中文。远端仓库 `OpenRealmCn/OpenVpn-DashBoard-Or`,主分支 `main`,直接推 main(无 PR 流程)。

## 常用命令

```bash
go run ./cmd/ovpn-web        # 启动面板 http://127.0.0.1:8686;非 Linux 自动走 mock 平台,全流程可演示
cd web && npm run dev        # 前端热更新(Vite 已代理 /api 与 /d);后端可配 -tags dev 构建跳过内嵌

go vet ./... && go test ./...   # 提交前必跑(与 CI 同口径)
cd web && npm run build         # 含 tsc --noEmit 类型检查;必须在提交前跑(见下)
```

- `web/dist/*` 已 gitignore,唯 `web/dist/index.html` 被跟踪且内容含构建哈希——改了前端就要 `npm run build` 并把它一并提交,否则内嵌构建用旧引用。
- `devdata*/` 是本地开发数据(含密钥凭据),严禁提交;`.cc-connect/` 同样已忽略。
- Linux 上强制 mock:`OVPN_MOCK=1`。

## 版本发行惯例(每个功能版本,缺一不可)

1. 本地验证:`go vet ./...`、`go test ./...`、`cd web && npm run build`。
2. 升级 `internal/version/version.go` 的 `Panel` 版本号;同步更新 README 的「功能特性」与「Linux 验收清单」。
3. 提交并推送,提交信息格式见下。
4. 在版本提交上打**轻量标签**并推送:`git tag vX.Y.Z <commit> && git push origin vX.Y.Z`(历来不用附注标签)。
5. 标签触发 `.github/workflows/release.yml`(约 2 分钟):前端构建 → vet/test → 交叉编译 linux amd64/arm64 → `gh release create --generate-notes` 上传二进制 + SHA256SUMS。
6. 核验:`gh run watch <run-id> --exit-status`;`gh release view vX.Y.Z --json tagName,assets` 确认 3 个资产齐全且为 Latest。完成报告应附 Release 结果。

提交信息格式(见 `git log`):

```
feat: <中文摘要> vX.Y.Z

<中文正文,按行宽手动折行,说明动机与行为变化>

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

## 架构要点

```
cmd/ovpn-web        入口:配置加载、平台选择、TLS 三模式(off/self/le)、-gen-node-token
internal/platform   系统抽象接口;linux/ 真实现,mock/ 内存态(Windows 全流程联调)
internal/installer  安装引擎:steps 串行调度、write-ahead journal、LIFO 回滚、SSE 日志
internal/dnsguard   resolved drop-in 管理与 UDP 53 占用归类
internal/api        Gin 路由(server.go 按权限分组)+ 各 handlers_*.go
internal/nodes      多节点:注册表、一次性绑定码、健康探测、代理请求构造
web/                React 18 + Vite + TS + Ant Design 5;types.ts 手工对齐 Go DTO 的 JSON
```

关键机制,改动时必须遵守:

- **write-ahead journal**:安装步骤对系统的任何改动,先 `Journal.Record` 落盘(fsync)再执行;新增改动类型要同时在 `journal.go` 定义动作、在 `rollback.go` 的 `undo` 实现撤销。回滚按 LIFO 逆序,失败也继续处理其余条目。
- **权限双层模型**:宿主机 `users.Perms`(六位)与节点授权 `users.NodeGrant`(full 或九位细分)彻底解耦。发往子节点的代理请求在 `handlers_nodes.go` 的 `permNeeded()` 按路径映射权限位——**新增子节点接口必须检查该映射**,未识别的写操作默认仅「完整管理」可执行。
- **管理目标切换**:前端 `target.tsx` 让仪表盘/证书页作用于宿主机或子节点,`apiPath()` 自动加代理前缀;新页面接入时用它而非硬编码 `/api/`。
- **mock 平台要跟上**:新增的系统行为(服务、端口、drop-in 效果等)需在 `platform/mock` 模拟(参考 `syncResolvedPort`),保证 Windows 演示模式走通全流程,这也是手工验证的主要途径。
- 前后端字段:Go 结构体 JSON tag 变了,`web/src/types.ts` 必须同步。

## 硬性安全规则(历史设计决策,不得放宽)

- 动 systemd-resolved 一律走 drop-in(`/etc/systemd/resolved.conf.d/99-openvpntools.conf`),**绝不改写主配置**;操作前先 `SnapshotStub` 记录原状。
- UDP 53 占用只归类不动手:resolved 可自动处理;已知 DNS 服务、未知进程**绝不停止**——后端刻意不存在该 API,前端也不得添加此类按钮。
- 仅支持全新安装:检测到 `server.conf` 即拒绝,避免破坏手工部署。
- 子节点 `node_token` 只存主节点数据目录(0600),API 层绝不外传。
- 面板不以 root 以外的方式绕过权限检查;所有外部输入在 `Params.Normalize` 等收口处校验。

## 已知易漏项

- 新增维护类接口要同时挂到 `server.go` 对应权限分组,并确认 `permNeeded()` 对节点代理路径的映射(`dns/`、`system/` 走「升级维护」)。
- README 与验收清单是功能文档的一部分,功能提交须同步更新。
- `gofmt` 只格式化自己改动的文件即可(仓库存量文件有未格式化的,勿顺手全改)。
