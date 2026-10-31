# 独立 VPN 订阅管理

部署目标为新跳板 `38.47.115.99`，管理入口为 `https://38.47.115.99/vpn/`。这是独立辅助服务，不修改 Sub2API 主应用、数据库或 staging/prod 镜像。入口使用 IP SAN 的受信任短期证书；Caddy 既有域名站点继续使用原配置。

## 功能和边界

- 后台管理 SOCKS5、HTTP、HTTPS、Shadowsocks、AnyTLS 节点，支持新增、编辑、删除、加入或移出订阅。订阅和后台列表中的显示名统一为“自定义名称 · IP/地址 · 地址名称”，例如“芝加哥 · 1.2.3.4 · 美国·伊利诺伊州·芝加哥”；新增或编辑时地址名称留空，将通过 HTTPS 调用 IPWho.is 中文地理库自动填入国家、州和城市；仅查询公网 IPv4/IPv6，域名和内网地址需手动填写。查询设置 4 秒网络超时，失败仍保存节点并提示重试。手动地址优先保留；自动地址在更换 IP 后重新查询，订阅读取不调用外部查询。旧记录补齐时仅更新空地址，数据库中的自定义名称不会被改写。地理库结果可能存在偏差，也不代表代理实际出口或住宅属性。
- 每个节点操作区提供“IP 检测”链接，将服务器地址编码为 `https://ippure.com/?ip=<IP>`，点击后在新标签页打开；不携带代理密码或订阅凭据。
- HTTP、HTTPS、SOCKS5 节点提供“复制账密”，按 `账:密@主机:端口` 原样复制保存的连接参数，不添加协议前缀、不转义账密；IPv6 主机使用方括号，复制成功提示对应协议。AnyTLS / Shadowsocks 不能直接用作 HTTP/SOCKS5 应用代理：须先部署兼容接口，将其作为独立节点添加，再在原节点的编辑弹窗中选择“复制应用账密时使用”。关联后原节点显示“复制应用账密”，读取关联节点的最新凭据；未关联时提示使用 VPN 订阅，不再输出缺失协议信息的账密串。关联只影响复制，VPN 订阅保持原协议、证书和中转链。
- `nodes.application_proxy_id` 保存应用代理关联；目标仅限 HTTP、HTTPS、SOCKS5，目标是否加入 VPN 订阅不影响复制。被关联的目标不能直接删除或改为不兼容协议，须先解除关联。普通编辑和旧客户端未提交此字段时保留原关联。仅在管理员点击时通过 `POST /vpn/api/nodes/{id}/connection` 返回 `connection` 和 `protocol`，接口沿用登录和同源校验且禁止缓存，列表不返回密码。浏览器拒绝自动复制时提供可重试和手动复制的弹窗，关闭即清空显示内容。复制内容不包含 VPN 中转链，应用按提示选择代理协议。
- 默认输出完整 Clash/Mihomo 配置；自建节点使用 AnyTLS，外部代理使用原协议。Shadowrocket 直连 Base64 格式仅在订阅不含 AnyTLS 时提供；当前自建节点已启用 AnyTLS，请使用 Clash/Mihomo。二维码由服务器本地生成，不调用第三方二维码服务。
- 后台复制链接和二维码默认使用 `https://38.47.115.99/s/<16位随机短码>`，短码具有 96 位随机强度，旧 `/vpn/sub/<长码>` 链接继续有效。后台重置链接时，新旧两种地址同时失效并生成新地址；短链接直接返回订阅内容，不依赖跳转或外部短网址服务。
- 节点来源区分为自建节点、surflare 住宅 IP、cc 静态代理和静态代理107。首次导入读取线上 `proxies` 表指定记录，之后独立管理，不会回写或自动同步线上代理池。
- 自建节点通过独立 sing-box 提供 AnyTLS，监听 TCP `24444`，使用 IP 证书加密；原 Shadowsocks 配置在服务器 root-only 目录保留作回滚参考。指定的三个外部代理默认由 Mihomo 经自建节点加密中转后再使用各自原协议，出口仍为对应代理 IP；后台可关闭中转改为直连。新添加节点默认勾选中转。有启用的中转节点时，不允许停用自建节点。SOCKS5/HTTP 节点是否支持 UDP 取决于供应商，本配置不声明其 UDP 支持。
- 订阅默认提供手动节点选择，局域网与中国 IP 直连，其余流量走所选节点；客户端需自行启用系统代理或 TUN 才接管设备流量。
- “加入订阅”仅控制订阅输出，不表示连通性健康，也不关闭节点服务器。修改后需在客户端更新订阅。重置链接可阻止旧 URL 后续下载，不能撤销已下载的节点凭据；如需撤销节点访问，另行轮换节点密码。Shadowrocket 直连格式不能表达上述中转链，当前导出器尚未实现 AnyTLS 分享链接；请使用已验收的 Clash/Mihomo 格式。
- cc 静态代理的来源记录已标记过期，但首次导入时 SOCKS5 出口实测可连接；应核对供应商续费。相同 IP 的另一条 HTTPS 记录握手失败，未导入。
- 管理员密码使用 scrypt，Cookie 使用 Secure/HttpOnly/SameSite，写操作校验 Origin；登录按来源限制尝试次数。节点密码不在列表接口中返回，订阅 URL 视作访问凭据。

## 运行目录

| 内容 | 路径 |
| --- | --- |
| Git 源码 | `/opt/sub-vpn/repo`，仅检出已推送的 `origin/main` |
| 当前程序 | `/opt/sub-vpn/current`，指向按完整 commit 归档的发布目录 |
| Python 环境 | `/opt/sub-vpn/venv` |
| 独立数据库与节点配置 | `/var/lib/sub-vpn/panel.db`、`sing-box.json`，服务用户可读写，文件权限 0600 |
| 初始管理员凭据 | `/root/sub-vpn-setup/admin-initial.txt`，仅 root 可读，不进入 Git |
| 自建节点程序 | `/usr/local/bin/sub-vpn-sing-box` |
| Caddy 追加配置 | `/etc/caddy/sub-vpn.caddy`，主配置通过 import 加载 |
| IP 证书源 | `/etc/letsencrypt/live/sub-vpn-ip/` |
| Caddy 证书副本 | `/var/lib/sub-vpn-tls/`，root:caddy，目录 0750、文件 0640 |
| AnyTLS 证书副本 | `/var/lib/sub-vpn/anytls-fullchain.pem`、`anytls-privkey.pem`，sub-vpn:sub-vpn，文件 0600 |
| 证书 HTTP 验证目录 | `/var/lib/sub-vpn-acme/` |

后台只监听 `127.0.0.1:18181`。三个 systemd 单元分别管理后台、自建节点和证书续期；续期 timer 每十二小时检查，成功后由 `install-ip-cert` 安装证书、校验 Caddy 并平滑 reload，若自建节点正在运行则校验配置并重启节点以加载新证书（会短暂中断节点连接）。IP 证书使用 Let's Encrypt `shortlived` profile，80/443 必须保持公网可达。

## 检查与维护

### 美国家宽节点与应用 SOCKS5

三个自建 VPS 为 `47.180.0.230`、`47.180.0.110`、`47.181.58.160`，VPN 节点名分别为“vps美国家宽-1 / -2 / -3”。AnyTLS 使用 TCP `24444`，沿用仅允许 `185.61.210.32` 和 `38.47.115.99` 的来源白名单，以及 VPN 后台现有中转设置。名称由用户指定，不作为住宅属性证明。

应用接口另用 TCP `24445`，提供 SOCKS5 用户名/密码认证，按用户授权允许任意 IPv4 来源。每台生成独立的应用凭据，不复用 root 密码或 AnyTLS 密码。SOCKS5 本身不提供传输加密；应用访问 HTTPS 时，目标 HTTPS 内容仍由 TLS 加密。VPN 客户端继续使用原 AnyTLS 订阅。

应用服务运行于独立 `sub-vpn-application-socks.service`，开机启动、异常重启，复用 `/usr/local/bin/sub-vpn-sing-box`；配置位于 `/var/lib/sub-vpn/application-socks.json`，`sub-vpn:sub-vpn`、0600。修改前保存受限备份，使用 `sub-vpn-sing-box check -c /var/lib/sub-vpn/application-socks.json` 校验后仅重启应用 SOCKS5 服务。停止此服务不影响 AnyTLS；不改动 AnyTLS 白名单。

SSH 使用 root 和各机专用密钥 `~/.ssh/sub2api_47_180_0_230_ed25519`、`~/.ssh/sub2api_47_180_0_110_ed25519`、`~/.ssh/sub2api_47_181_58_160_ed25519`，需要时经 `-J sub2api-migration-vps` 连接。后台为三个应用接口建立独立 SOCKS5 记录，原 AnyTLS 节点通过 `application_proxy_id` 关联；应用记录不加入 VPN 订阅，显示“未加入”仅表示订阅状态，接口仍运行。

验收须包含实际 HTTPS 出口 IP、无凭据及错误凭据拒绝、后台复制结果连通性，以及新增前后 VPN 订阅一致性。停用应用接口时先解除原节点的应用代理关联、处理应用中使用的凭据，再删除应用记录并停止 `sub-vpn-application-socks`。数据库变更前使用 SQLite backup 接口备份至后台主机 root-only 目录；回滚应用关联时保留现有 VPN 节点和其他运行数据。

### 外部自建节点：小鸡家宽

`192.220.36.75:24444` 使用独立 sing-box AnyTLS 入站，作为普通可编辑节点加入管理后台，`relay=false`，客户端直接连接该机。显示名为“小鸡家宽 · 192.220.36.75 · 美国·加利福尼亚州·洛杉矶”。此名称由用户指定；出口查询归属 AS2914 NTT America，不能据此确认住宅属性。

SSH 使用 `~/.ssh/sub2api_192_220_36_75_ed25519`。程序为 `/usr/local/bin/sub-vpn-sing-box`，配置和证书副本在 `/var/lib/sub-vpn/`，仅服务用户可读，运行单元复用 `sub-vpn-node.service`。本机不运行订阅后台，不改变现有域名上游。

本节点使用包含 IP SAN 的独立自签证书，客户端通过可信 SSH 获取的 SHA-256 证书指纹固定身份。后台“服务器证书指纹”字段仅支持 AnyTLS；Mihomo 使用指纹校验替代公共 CA 信任链，不能移除指纹或改成接受任意证书。原跳板节点继续使用公共 CA 证书。证书、私钥、节点密码仅保留于服务器受限目录。

本机 80 端口的公共 CA 验证存在连接超时，未启用 IP ACME 签发或续期任务，也不修改原有网站证书。安装 `sub-vpn-external-cert-check.timer` 每日检查节点证书；剩余有效期不足 30 天时对应 service 失败，需管理员处理。该检查不自动续期、不主动发通知。证书有效期一年；轮换前生成新证书并在后台更新指纹，切换后要求客户端更新订阅，再测试实际流量，旧订阅中的指纹无法验证新证书。

维护时检查 `systemctl status sub-vpn-node sub-vpn-external-cert-check.timer`，使用 `openssl x509 -in /var/lib/sub-vpn/anytls-fullchain.pem -noout -dates -fingerprint -sha256` 核对证书。连接验收包括真实 Mihomo HTTPS 出口验证和错误指纹拒绝验证。后台独立 SQLite 修改前使用 SQLite backup 接口保存 root-only 一致性备份。

停用此节点时先在后台移出订阅，再停止本机 `sub-vpn-node` 和 `sub-vpn-external-cert-check.timer`。保留受限配置与证书以便恢复，不能关闭既有网站服务。

```bash
ssh -i ~/.ssh/sub2api_38_47_115_99_ed25519 root@38.47.115.99
systemctl status sub-vpn-panel sub-vpn-node sub-vpn-cert-renew.timer
curl --fail https://38.47.115.99/vpn/health
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
```

本地接口验证：

```bash
python3 -m venv /tmp/sub-vpn-test-venv
/tmp/sub-vpn-test-venv/bin/pip install -r tools/vpn-panel/requirements.txt httpx==0.28.1
/tmp/sub-vpn-test-venv/bin/python -m unittest discover -s tools/vpn-panel -p 'test_*.py' -v
node --check tools/vpn-panel/static/app.js
```

更新先完成本地检查、提交并推送 main，再在跳板独立源码目录 fetch、fast-forward main，按同一完整 SHA 创建发布目录并切换 `current`。不运行主应用的发布脚本，也不自动更新源码。运行数据不随代码覆盖。

修改 Caddy 前备份至 `/root/sub-vpn-setup/`；必须通过 validate 后才 reload，并回归四个原有 shop/画布域名。回滚本辅助服务时停用 `sub-vpn-panel`、`sub-vpn-node` 和证书 timer，移除新增 import 后 validate/reload；保留运行数据库和证书用于恢复，不能覆盖既有域名配置。后台更新回滚切换 `current` 到先前发布目录并重启后台。

初次初始化由 root 使用受限文件运行 `bootstrap.py`，若数据库已有管理配置会拒绝覆盖。不要在命令参数、日志、提交或文档中填写真实代理凭据或管理员密码。

AnyTLS 已使用 Clash Verge 内置 Mihomo v1.19.19 与 sing-box 1.14.3 实测通过，旧版原始 Clash 内核不适用。四个节点均通过明确选择节点后的 HTTPS 出口验证。原 Shadowsocks 24443 入口仍运行，供已下载旧配置的客户端过渡；后台自建节点当前使用 AnyTLS 24444。初次 bootstrap 仍创建 Shadowsocks 基线，AnyTLS 升级需先安装证书和入站、验证后再修改自建记录。
