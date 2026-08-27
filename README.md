# OpenVpn-DashBoard-Or — OpenVPN Web 管理面板

仓库:[OpenRealmCn/OpenVpn-DashBoard-Or](https://github.com/OpenRealmCn/OpenVpn-DashBoard-Or) · 许可证:MIT

部署在 Debian/Ubuntu 服务器上的单二进制 Web 面板,覆盖 OpenVPN 从安装到日常运维的完整流程,并修正常见一键脚本的痛点。可从 [Releases](https://github.com/OpenRealmCn/OpenVpn-DashBoard-Or/releases) 直接下载 linux/amd64 与 linux/arm64 产物。

## 功能

- **交互式安装向导**:自定义监听端口/协议/VPN 网段/推送 DNS/公网地址,SSE 实时日志,现代加密默认值(`tls-crypt`、AEAD、EC 证书、`dh none`)
- **失败自动回滚**:write-ahead 回滚 journal,任一步失败按 LIFO 逆序恢复系统原状(文件、软件包、sysctl、防火墙规则、DNS 改动)
- **DNS 安全处理**:
  - 关闭 systemd-resolved 的 DNS Stub 一律通过 **drop-in**(`/etc/systemd/resolved.conf.d/99-openvpntools.conf`),绝不改写主配置,可一键恢复原状
  - UDP 53 占用自动归类:resolved / 已知 DNS 服务 / 未知进程;**未知进程绝不自动停止**(后端不存在该 API)
- **客户端证书管理**:秒建无密码或加密私钥证书、列表(V/R/E 状态与到期时间)、一键吊销(revoke + gen-crl,新连接立即拒绝)
- **在线客户端**:实时列表(来源地址/VPN 地址/上下行流量/连接时长,解析 status.log),经 management unix socket 一键断开会话
- **服务控制与版本更新**:面板内启动/停止/重启 OpenVPN;检查并升级 OpenVPN(apt)与 EasyRSA(GitHub 官方 SHA256 digest 校验 + bash -n + 完整保留 PKI,失败自动回退旧版本)
- **二维码分享**:一次性高熵下载链接(默认 10 分钟、单次有效),手机扫码免登录导入 `.ovpn`
- **状态检查**:服务状态、实际监听端口、IPv4 转发(runtime + 持久化)一键修复
- **可靠下载**:EasyRSA 从 GitHub 拉取,TLS ≥ 1.2、指数退避重试、内置 SHA256 校验(可配镜像,镜像不可信也安全)、tar 防路径穿越、`bash -n` 语法检查后才执行
- **子用户与权限**:管理员可创建子用户,按「查看 / 创建证书 / 吊销证书 / 安装 / 断开客户端 / 系统维护」六项细粒度授权,可随时修改并即时生效;每个子用户可设**有效证书数量上限**(0 = 不限);子用户只能下载/分享/吊销自己创建的证书,证书列表显示创建者
- **面板安全**:首启强制设管理员密码(bcrypt)、JWT httpOnly Cookie、登录与下载限流、持久化审计日志(谁在何时做了什么,面板内可查)、自助修改密码、可选自签 HTTPS
- **运维便利**:证书有效期可自定义(1-3650 天);一键导出备份(PKI + 配置 + 用户数据 tar.gz),支持上传恢复(被替换文件自动留底)
- **IPv6**:安装向导可选启用(server-ipv6 + NAT66 + v6 转发持久化),为客户端提供 v6 出口
- **面板 HTTPS 三模式**:关闭 / 自签名 / Let's Encrypt(绑定域名自动签发续期受信证书)
- **面板自更新**:从 GitHub Release 检查新版,校验 GitHub 官方 SHA256 digest 后原子替换二进制,一键重启生效(systemd Restart=always)

## 构建

依赖:Go 1.22+、Node 18+。

```powershell
# Windows(开发机)
.\build.ps1        # 前端 + 测试 + 交叉编译出 dist/ovpn-web-linux-{amd64,arm64}
```

```bash
# Linux / macOS
make               # 等价:make web && make linux
```

本地开发(Windows 即可,mock 平台不触碰真实系统):

```powershell
go run ./cmd/ovpn-web          # API + 内嵌前端,http://127.0.0.1:8686
# 前端热更新:另开终端
cd web; npm run dev            # Vite :5173,已代理 /api 与 /d
```

## 部署(Debian 11+ / Ubuntu 20.04+)

方式一:管理脚本(从 GitHub Release 拉取,SHA256SUMS 强校验):

```bash
# 安装或更新(自动判断)
curl -fsSL https://raw.githubusercontent.com/OpenRealmCn/OpenVpn-DashBoard-Or/main/install.sh | sudo bash
```

脚本子命令(安装后本机也可直接 `sudo ovpn-ctl` 进交互菜单):

```bash
SCRIPT=https://raw.githubusercontent.com/OpenRealmCn/OpenVpn-DashBoard-Or/main/install.sh
curl -fsSL $SCRIPT | sudo bash -s -- install v0.3.0        # 安装指定版本
curl -fsSL $SCRIPT | sudo bash -s -- update                # 更新面板(配置数据不动)
curl -fsSL $SCRIPT | sudo bash -s -- status                # 查看状态
curl -fsSL $SCRIPT | sudo bash -s -- uninstall --yes       # 卸载面板(保留数据与 OpenVPN)
curl -fsSL $SCRIPT | sudo bash -s -- uninstall --purge --all --yes
                                   # 面板 + 数据 + OpenVPN 部署(证书/防火墙规则/DNS drop-in)全部清理
# 国内镜像下载(校验和优先直连 GitHub,镜像不可信也安全):
GH_MIRROR=https://your-mirror.example curl -fsSL $SCRIPT | sudo GH_MIRROR=https://your-mirror.example bash
```

方式二:自行构建后拷贝安装:

```bash
scp dist/ovpn-web-linux-amd64 scripts/ovpn-web.service scripts/install-panel.sh root@server:/tmp/
ssh root@server 'cd /tmp && sh install-panel.sh ./ovpn-web-linux-amd64'
```

浏览器打开 `http://<服务器IP>:8686`:

1. 首次访问设置管理员密码(≥ 8 位)
2. 「安装向导」填写端口等参数 → 预检通过后开始安装
3. 「客户端证书」创建证书 → 下载 `.ovpn` 或点「二维码」手机扫码导入
4. 「仪表盘」检查服务/端口/转发状态,处理 DNS Stub

安全建议:用防火墙限制面板端口来源;或在「面板设置」开启 HTTPS(自签)后通过 `https://` 访问;扫码需手机可达面板地址,可在设置页配置 `panel_url`。

## 配置

默认 `/etc/openvpntools/config.yaml`(首启自动生成),示例见 `configs/example.yaml`。数据目录 `/var/lib/openvpntools` 存放回滚 journal、安装参数与自签证书。

## Linux 验收清单(建议在测试 VM 全量过一遍)

- [ ] 纯净 Ubuntu 22.04 / Debian 12 向导安装成功,客户端连通且 NAT 出网
- [ ] 真机扫码导入 `.ovpn`,连接成功;同一链接第二次访问返回 410
- [ ] 带密码证书导入时要求输入密码
- [ ] 吊销后原客户端无法建立新连接
- [ ] 人为制造失败(如安装中途占用端口/断网)→ 自动回滚后:`/etc/openvpn`、`/etc/sysctl.d`、iptables 规则、resolved drop-in 与 `/etc/resolv.conf` 均复原
- [ ] dnsmasq 或 `nc -lup 53` 占用 53 时,预检正确归类且不停止对方进程
- [ ] 仪表盘关闭 DNS Stub(drop-in 生成、53 释放、系统解析正常),恢复原状成功
- [ ] 未登录访问所有 `/api/*` 返回 401
- [ ] 子用户:仅授予「查看+创建证书」时,吊销/安装/维护接口返回 403;配额用满后创建被拒;只能下载自己创建的证书
- [ ] 管理员修改子用户权限后,子用户无需重新登录即生效;禁用账号后其会话立即失效

## 技术栈与结构

Go 1.22 + Gin(单二进制,`go:embed` 内嵌前端)/ React 18 + Vite + TypeScript + Ant Design 5(中文)。

```
cmd/ovpn-web        入口
internal/platform   系统抽象(linux 真实现 + mock,Windows 可全流程演示)
internal/installer  安装引擎:steps、write-ahead journal、回滚、SSE 日志
internal/dnsguard   resolved drop-in 管理、UDP 53 归类保护
internal/download   GitHub 下载器(重试/TLS/SHA256/防穿越/bash -n)
internal/easyrsa    EasyRSA 封装与 index.txt 解析
internal/clients    证书业务:创建/吊销/.ovpn 生成
internal/qrlink     一次性下载 token
web/                前端
scripts/            systemd unit 与部署脚本
```

## 路线图与建议(欢迎按需实现)

- **流量统计与图表**:定期采样 status.log 落盘(SQLite 或时序文件),仪表盘展示每客户端历史流量曲线
- **通知集成**:证书临近到期、服务掉线、安装失败时推送 Webhook / Telegram / 邮件
- **TOTP 两步验证**:面板登录加一层动态口令
- **客户端进阶**:ccd 固定 IP 分配、client-to-client 开关、按客户端限速(tc)
- **多节点管理**:一个面板纳管多台 VPN 服务器(agent 模式)
- **WireGuard 支持**:复用现有面板与用户体系,增加 wg 协议栈

## 已知限制

- 仅支持全新安装(检测到已有 `server.conf` 时拒绝,避免破坏手工部署)
- `self` DNS 模式(resolved 服务 VPN 客户端)需要 systemd ≥ 247(Debian 11+ / Ubuntu 22.04+)
- 分享 token 为内存态,面板重启后需重新生成(设计如此)
- "断开在线客户端"依赖 server.conf 中的 management 配置,由本面板安装的实例自带;手工部署的旧配置不支持(界面会提示)
