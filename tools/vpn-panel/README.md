# 独立 VPN 订阅管理

部署目标为新跳板 `38.47.115.99`，管理入口为 `https://38.47.115.99/vpn/`。这是独立辅助服务，不修改 Sub2API 主应用、数据库或 staging/prod 镜像。入口使用 IP SAN 的受信任短期证书；Caddy 既有域名站点继续使用原配置。

## 功能和边界

- 后台管理 SOCKS5、HTTP、HTTPS、Shadowsocks、AnyTLS 节点，支持新增、编辑、删除、加入或移出订阅。
- 默认输出完整 Clash/Mihomo 配置；自建节点使用 AnyTLS，外部代理使用原协议。Shadowrocket 直连 Base64 格式仅在订阅不含 AnyTLS 时提供；当前自建节点已启用 AnyTLS，请使用 Clash/Mihomo。二维码由服务器本地生成，不调用第三方二维码服务。
- 后台复制链接和二维码默认使用 `https://38.47.115.99/s/<16位随机短码>`，短码具有 96 位随机强度，旧 `/vpn/sub/<长码>` 链接继续有效。后台重置链接时，新旧两种地址同时失效并生成新地址；短链接直接返回订阅内容，不依赖跳转或外部短网址服务。
- 节点来源区分为自建节点、surflare 住宅 IP、cc 静态代理和静态代理107。首次导入读取线上 `proxies` 表指定记录，之后独立管理，不会回写或自动同步线上代理池。
- 自建节点通过独立 sing-box 提供 AnyTLS，监听 TCP `24444`，使用 IP 证书加密；原 Shadowsocks 配置在服务器 root-only 目录保留作回滚参考。指定的三个外部代理默认由 Mihomo 经自建节点加密中转后再使用各自原协议，出口仍为对应代理 IP；后台可关闭中转改为直连。新添加节点默认勾选中转。有启用的中转节点时，不允许停用自建节点。SOCKS5/HTTP 节点是否支持 UDP 取决于供应商，本配置不声明其 UDP 支持。
- 订阅默认提供手动节点选择，局域网与中国 IP 直连，其余流量走所选节点；客户端需自行启用系统代理或 TUN 才接管设备流量。
- “加入订阅”仅控制订阅输出，不表示连通性健康，也不关闭节点服务器。修改后需在客户端更新订阅。重置链接可阻止旧 URL 后续下载，不能撤销已下载的节点凭据；如需撤销节点访问，另行轮换节点密码。Shadowrocket 直连格式不能表达上述中转链，也不支持当前 AnyTLS 自建节点；请使用已验收的 Clash/Mihomo 格式。
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

后台只监听 `127.0.0.1:18181`。三个 systemd 单元分别管理后台、自建节点和证书续期；续期 timer 每十二小时检查，成功后由 `install-ip-cert` 安装证书、校验 Caddy 并平滑 reload。IP 证书使用 Let's Encrypt `shortlived` profile，80/443 必须保持公网可达。

## 检查与维护

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
