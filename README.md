<div align="center">

# OpenVpn-DashBoard-Or

**OpenVPN Web 管理面板** · 单二进制部署,安装、证书、用户、更新一站式搞定

[![Release](https://img.shields.io/github/v/release/OpenRealmCn/OpenVpn-DashBoard-Or)](https://github.com/OpenRealmCn/OpenVpn-DashBoard-Or/releases)
[![Build](https://img.shields.io/github/actions/workflow/status/OpenRealmCn/OpenVpn-DashBoard-Or/release.yml?label=release%20build)](https://github.com/OpenRealmCn/OpenVpn-DashBoard-Or/actions)
[![License](https://img.shields.io/github/license/OpenRealmCn/OpenVpn-DashBoard-Or)](LICENSE)
![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/platform-Debian%2011%2B%20%7C%20Ubuntu%2020.04%2B-orange)

[快速开始](#快速开始) · [功能特性](#功能特性) · [管理脚本](#管理脚本) · [配置](#配置) · [开发与构建](#开发与构建) · [路线图](#路线图)

</div>

---

## 快速开始

在 Debian 11+ / Ubuntu 20.04+ 服务器上执行(root):

```bash
curl -fsSL https://raw.githubusercontent.com/OpenRealmCn/OpenVpn-DashBoard-Or/main/install.sh | sudo bash
```

然后浏览器打开 `http://<服务器IP>:8686`:

1. 设置管理员密码(≥ 8 位)
2. 「安装向导」填好端口/协议/DNS → 预检通过后一键安装 OpenVPN
3. 「客户端证书」创建证书 → 下载 `.ovpn` 或手机扫码导入

> 安全建议:用防火墙限制面板端口来源,或在「面板设置」开启 HTTPS(自签 / Let's Encrypt)。

## 功能特性

**安装与回滚**

- 交互式安装向导:自定义端口、UDP/TCP、VPN 网段、可选 IPv6(NAT66)、五种客户端 DNS 模式,SSE 实时日志
- write-ahead 回滚 journal:任一步失败按 LIFO 逆序恢复原状(文件、软件包、sysctl、防火墙规则、DNS 改动),面板重启后残留可继续回滚
- 现代加密默认值:`tls-crypt`、AEAD(AES-GCM)、EC 证书、`dh none`、CRL 校验

**DNS 安全(不做鲁莽操作)**

- 关闭 systemd-resolved 的 DNS Stub 一律走 **drop-in**(`/etc/systemd/resolved.conf.d/`),绝不改写主配置,可一键恢复
- UDP 53 占用自动归类:resolved / 已知 DNS 服务 / 未知进程;**未知进程绝不自动停止**(后端不存在该 API)
- VPN 端口选 53 时可勾选「自动关闭 DNS Stub」:安装器确认占用者仅为 resolved 后,在启动服务前经 drop-in 关闭 Stub 释放端口并切换 `resolv.conf` 到真实上游;原状记入回滚 journal,失败或回滚自动复原,其它进程占用仍会中止安装

**证书与分享**

- 秒建无密码或加密私钥证书,有效期可自定义(1-3650 天);吊销即 `revoke + gen-crl`,新连接立即被拒
- 二维码一次性分享:高熵 token、默认 10 分钟、单次有效,手机扫码免登录导入
- 在线客户端列表(来源/VPN 地址/流量/时长),经 management 接口一键断开

**多用户与安全**

- 子用户细粒度权限(查看/建证书/吊销/安装/断开/维护),证书数量配额,修改即时生效
- 子用户只能操作自己创建的证书;持久化审计日志;登录与下载限流;bcrypt + JWT httpOnly Cookie

**多节点管理**

- 一个主面板纳管多台 VPN 服务器:健康总览(版本/服务/在线数)、节点详情(服务控制/远程安装/证书管理)
- 快捷绑定:主节点生成一次性绑定码(15 分钟),子节点一行命令完成安装 + 令牌生成 + 自注册
- 远程安装:主面板向单个或多个子节点下发 OpenVPN 部署(连接地址自动取各节点主机名),进度实时可见
- 批量下发:勾选多节点一键执行重启服务、检查更新、升级 OpenVPN/EasyRSA/面板等,结果逐节点可读展示
- 节点授权:子用户按节点授予「完整管理」模板或九项细分权限(查看/创建证书/吊销/安装/回滚/服务控制/断开客户端/升级维护/重启面板),附只读、证书管理员、运维等预设模板;主面板逐操作校验,修改即时生效
- 节点 DNS 管理:节点详情新增「DNS Stub / 53 端口」页,远程查看子节点 resolved 状态与 53 端口占用归类,一键关闭 / 恢复 DNS Stub(操作需「升级维护」授权)

**运维**

- 服务启停、IPv4/IPv6 转发一键持久化、状态总览
- 三级更新:OpenVPN(apt)、EasyRSA(GitHub Release + 官方 SHA256 digest 校验、保留 PKI)、面板自更新(原子替换 + 一键重启)
- 备份导出 / 上传恢复(被替换文件自动留底);所有 GitHub 下载:TLS ≥ 1.2、退避重试、SHA256 强校验、tar 防穿越、`bash -n` 后才执行

## 管理脚本

`install.sh` 同时是管理脚本(安装后本机可直接 `sudo ovpn-ctl` 进交互菜单):

```bash
SCRIPT=https://raw.githubusercontent.com/OpenRealmCn/OpenVpn-DashBoard-Or/main/install.sh

curl -fsSL $SCRIPT | sudo bash                              # 安装或更新(自动判断)
curl -fsSL $SCRIPT | sudo bash -s -- install v0.3.0         # 安装指定版本
curl -fsSL $SCRIPT | sudo bash -s -- update                 # 更新面板(配置数据不动)
curl -fsSL $SCRIPT | sudo bash -s -- status                 # 查看状态
curl -fsSL $SCRIPT | sudo bash -s -- uninstall --yes        # 卸载面板(保留数据与 OpenVPN)
curl -fsSL $SCRIPT | sudo bash -s -- uninstall --purge --all --yes
                        # 面板 + 数据 + OpenVPN 部署(证书/防火墙规则/DNS drop-in)全部清理
curl -fsSL $SCRIPT | sudo bash -s -- join http://主节点:8686 XXXX-XXXX-XXXX
                        # 绑定为子节点(绑定码在主节点「节点管理 → 添加子节点」生成)
```

国内镜像下载(校验和优先直连 GitHub,镜像不可信也安全):

```bash
GH_MIRROR=https://your-mirror.example curl -fsSL $SCRIPT | sudo GH_MIRROR=https://your-mirror.example bash
```

离线场景可用 `scripts/install-panel.sh` 从本地二进制安装。

## 配置

- 面板配置:`/etc/openvpntools/config.yaml`(首启自动生成,示例见 [configs/example.yaml](configs/example.yaml))
- 数据目录:`/var/lib/openvpntools`(回滚 journal、子用户、审计日志、自签证书等)
- HTTPS 三模式:关闭 / 自签名 / Let's Encrypt(绑定域名,自动签发续期,强制监听 443 并占用 80 做验证)
- 服务管理:`systemctl status|restart ovpn-web`(单元为 `Restart=always`,支持面板自更新后自动拉起)

## 开发与构建

依赖 Go 1.22+ 与 Node 18+;Windows 上可完整开发调试(mock 平台不触碰真实系统):

```bash
go run ./cmd/ovpn-web        # http://127.0.0.1:8686,mock 模式可演示全部流程
cd web && npm run dev        # 前端热更新(已代理 /api 与 /d)
```

发布构建:

```bash
make                         # Linux/macOS:前端 + linux/{amd64,arm64}
.\scripts\build.ps1          # Windows 同上,并生成 SHA256SUMS
```

推送 `v*` 标签会触发 GitHub Actions 自动构建并发布 Release(含 `SHA256SUMS`)。

<details>
<summary><b>项目结构</b></summary>

```
cmd/ovpn-web        入口(HTTP/自签/Let's Encrypt)
internal/platform   系统抽象(linux 真实现 + mock,Windows 可全流程演示)
internal/installer  安装引擎:steps、write-ahead journal、回滚、SSE 日志
internal/dnsguard   resolved drop-in 管理、UDP 53 归类保护
internal/download   GitHub 下载器(重试/TLS/SHA256/防穿越/bash -n)
internal/easyrsa    EasyRSA 封装与 index.txt 解析
internal/clients    证书业务:创建/吊销/.ovpn/在线列表/归属配额
internal/users      子用户与权限
internal/updates    OpenVPN/EasyRSA/面板 三级更新
internal/backup     备份恢复
internal/audit      审计日志
web/                React 18 + Vite + TS + Ant Design 5(中文)
scripts/            systemd 单元、构建与离线安装脚本
install.sh          在线管理脚本(安装/更新/卸载/状态)
```

</details>

<details>
<summary><b>Linux 验收清单</b>(建议在测试 VM 全量过一遍)</summary>

- [ ] 纯净 Ubuntu 22.04 / Debian 12 向导安装成功,客户端连通且 NAT 出网
- [ ] 启用 IPv6 安装后,客户端获得 v6 地址且可经 NAT66 出网
- [ ] 真机扫码导入 `.ovpn`,连接成功;同一链接第二次访问返回 410
- [ ] 带密码证书导入时要求输入密码;吊销后原客户端无法建立新连接
- [ ] 人为制造失败(占用端口/断网)→ 自动回滚后 `/etc/openvpn`、`/etc/sysctl.d`、iptables、resolved drop-in 与 `/etc/resolv.conf` 均复原
- [ ] dnsmasq 或 `nc -lup 53` 占用 53 时,预检正确归类且不停止对方进程
- [ ] 端口选 53 且被 resolved Stub 占用:未勾选自动释放时预检给出提示;勾选后安装成功、本机解析正常,回滚后 Stub 与 `resolv.conf` 复原
- [ ] 仪表盘关闭 DNS Stub(drop-in 生成、53 释放、解析正常),恢复原状成功;节点详情「DNS Stub / 53 端口」页对子节点可完成同样操作
- [ ] 子用户:仅授予「查看+创建证书」时,吊销/安装/维护接口 403;配额用满创建被拒;只能下载自己创建的证书
- [ ] 管理员修改子用户权限即时生效;禁用账号后其会话立即失效
- [ ] 备份导出 → 篡改/删除文件 → 上传恢复后完整复原,OpenVPN 自动重启
- [ ] 面板自更新到新 Release 后一键重启生效(systemd 自动拉起)
- [ ] 未登录访问所有 `/api/*` 返回 401

</details>

## 路线图

- 流量统计与历史图表(采样 status.log 落盘,仪表盘曲线)
- 通知集成:证书到期、服务掉线、安装失败推送 Webhook / Telegram / 邮件
- TOTP 两步验证
- 客户端进阶:ccd 固定 IP、client-to-client 开关、按客户端限速
- WireGuard 协议支持

## 已知限制

- 仅支持全新安装(检测到已有 `server.conf` 时拒绝,避免破坏手工部署)
- `self` DNS 模式(resolved 服务 VPN 客户端)需要 systemd ≥ 247(Debian 11+ / Ubuntu 22.04+)
- 分享 token 为内存态,面板重启后需重新生成(设计如此)
- 「断开在线客户端」依赖本面板写入的 management 配置,手工部署的旧配置不支持(界面会提示)

## License

[MIT](LICENSE) © OpenRealmCn
