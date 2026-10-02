# shop 跳板运行说明

## 主机与访问

新跳板已具备两个 fast 域名及画布域名的接流条件，DNS 由用户切换。跳板只转发 HTTPS 请求，不运行本项目正式数据库，也不执行本项目的 staging/prod 发布。

| 角色 | 地址 | 本机 SSH 别名 | 密钥 |
| --- | --- | --- | --- |
| 新 shop 跳板 | `166.88.36.223` | `sub2api-shop-vps` | `~/.ssh/sub2api_166_88_36_223_ed25519` |
| 旧 shop 跳板 | `207.57.145.15` | `sub2api-jump-vps` | `~/.ssh/id_ed25519` |
| 正式应用上游 | `185.61.210.32` | `sub2api-migration-vps` | `~/.ssh/sub2api_185_61_210_32_ed25519` |

以上 SSH 登录账户均为 `root`。密码不保存到配置或文档中。

新机为 Ubuntu 26.04，约 2GiB 内存、40GiB 根磁盘；已有 Caddy 2.11.4、`mogai.youkeduo.shop` 和其他业务容器。shop 入口追加到现有 `/etc/caddy/Caddyfile`，复用 Caddy 的 80/443 监听。不得另起 Nginx 抢占端口，不得重装或重建既有应用。`mogai.youkeduo.shop` 仍代理到该机 `127.0.0.1:8080`，与 shop 跳板的远程上游不同。

## 反代与证书

现有 DNS 的另一个入口 `192.220.36.75` 使用 Nginx。`/etc/nginx/sites-available/fast-youkeduo-shop` 和 `fast-yukeduo-shop` 的 HTTPS 业务上游均为 `https://fast.youkeduo.xyz`，保留原始 Host，并以 `fast.youkeduo.xyz` 进行上游 TLS 证书校验，不写死正式机 IP。`proxy_pass` 不附加 URI，保留请求路径及查询参数。当前 Nginx 1.18 在加载配置时解析该域名；源站 DNS 地址变化后，需要执行 `nginx -t` 和 `systemctl reload nginx` 更新解析。源站域名必须直达正式应用，不能解析回跳板形成循环。HTTP 普通请求重定向至 HTTPS，ACME challenge 仍转发到 `207.57.145.15`。该机的 canvas、canvas2、账号管理和许可证站点独立管理，不能对全部配置批量替换地址。

该入口的域名上游配置回滚材料位于 root-only 目录 `/root/sub2api-jump-backups/20261002T021715Z-domain/`，包含两个原配置及验收结果；恢复后上游为新正式机 IP。更早的旧正式机上游配置保留于 `/root/sub2api-jump-backups/20261002T021209Z/`。恢复前先核对之后是否有其他配置变更；恢复对应配置后执行 `nginx -t` 和平滑 reload。重载后应等待新 worker 生效，再分别用 `curl --resolve` 检查两个 DNS 地址的 `/api/v1/settings/public` 版本、TLS、页面和鉴权响应；单独 `/health` 成功不能证明各入口运行版本一致。

以下证书表描述新跳板 `166.88.36.223`。

| 域名 | HTTPS 上游 | 上游 TLS 校验名称 | 当前证书到期时间（北京时间） |
| --- | --- | --- | --- |
| `fast.youkeduo.shop` | `185.61.210.32:443` | `fast.youkeduo.shop` | 2026-12-02 15:41:29 |
| `fast.yukeduo.shop` | `185.61.210.32:443` | `fast.youkeduo.shop` | 2026-12-02 15:16:25 |
| `canvas.youkeduo.shop` | `185.61.210.32:443` | `canvas.youkeduo.shop` | 2026-12-05 19:12:02 |

请求保留原始 Host，使用系统信任链校验上游证书，不启用跳过 TLS 校验。两个 fast 域名共用上游 TLS 名称是旧跳板的既有行为，客户端看到的证书仍分别匹配自己的域名。

反代保留 200MiB 请求体上限、30 秒连接超时和 3600 秒上游读写超时；关闭响应缓冲，支持 SSE 和 WebSocket 转发。`CF-Connecting-IP`、`X-Real-IP` 使用连接来源地址，`X-Forwarded-For` 由 Caddy 默认可信代理规则生成，不直接信任客户端伪造值。HTTP 自动以 308 跳转到同域名 HTTPS，保留路径和查询参数。

旧机 `/etc/nginx/jump-certs/` 中的三个证书和私钥经 SSH 加密流传到新机，校验域名、有效期和公私钥匹配后，导入运行中 Caddy 对应的托管存储格式：

```text
/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/<域名>/
  <域名>.crt
  <域名>.key
  <域名>.json
```

域名目录权限为 `0700`，文件为 `0600`。Caddy 运行配置已为三个域名启用 Let's Encrypt 自动管理，未使用会阻止自动续期的手工 `tls <证书> <私钥>` 配置。续期使用新机自己的 ACME 账户，不需要复制旧机账户密钥。DNS 切换后，80/443 必须持续可达，不能让旧 A/AAAA 记录把 ACME 校验导向其他机器。

已验证现有证书可以直接提供可信 HTTPS，以及 Caddy 已加载自动管理策略；DNS 尚未切换，因此尚未实测新机的公网 ACME 签发/续期。当前证书覆盖迁移过渡期，后续续期由 Caddy 完成。证书到期时间以上表的迁入证书为准，续期后需读取实际证书核实。

## DNS 切换

2026-10-02 核实时，三个域名均各有以下两条 A 记录，没有 AAAA 或 CNAME：

- `207.57.145.15`
- `192.220.36.75`

切换某个域名时，将该域名的 A 记录最终整理为**仅 `166.88.36.223`**，同时移除上述两条旧 A 记录。不要只增加新 IP，否则流量仍会随机进入旧机。保持当前 DNS-only 方式即可；迁移不依赖开启 CDN。若切换时出现额外 AAAA 或 CNAME，也必须先核实，不能继续指向旧入口。

两个 fast 域名及 canvas 均可切换。画布应用已迁至正式上游，Nginx 已改为固定宿主机端口，原有 502 已修复；新跳板、两个现有 DNS 入口及正式上游均通过 HTTP 200 验证。应用运行与数据恢复见 [画布与账号管理运行说明](AUXILIARY_APPS_CN.md)。

DNS 生效时间受记录 TTL 和客户端缓存影响。旧入口应至少保留到原记录 TTL 结束且业务验证通过；回滚时恢复该域名原来的两条 A 记录。切换后确认解析结果、证书和实际请求，不能仅依据 DNS 控制台保存成功判断完成。

## 验收

2026-10-02 使用 `curl --resolve` 直连新 IP，保留域名 SNI 和 Host，未跳过证书校验：

| 验证项 | 结果 |
| --- | --- |
| 两个 fast 域名的 `/`、`/health`、`/purchase`、`/admin/accounts` | HTTP 200，TLS 校验通过，协商 HTTP/2 |
| 两个 fast 域名的 `/models`、`/v1/models`、`/api/v1/admin/accounts` | 无凭据请求返回 HTTP 401 |
| 三个域名的 HTTP 请求 | HTTP 308，跳转路径和查询参数正确 |
| 三个域名的线上证书 | SHA-256 指纹与旧跳板一致 |
| 同机 `mogai.youkeduo.shop` | HTTP 200，TLS 校验通过 |
| 既有容器 | 修改前后容器 ID、镜像一致 |
| `canvas.youkeduo.shop` | 画布迁移后，新入口、两个现有 DNS 入口和正式上游均返回 HTTP 200；应用管理员登录及用户列表读取通过 |

跳板验证未使用 Sub2API 管理员凭据或付费模型调用；Sub2API 管理端登录后的操作、实际模型流式响应和 WebSocket 会话没有进行端到端验证。画布管理员登录和只读列表已在应用迁移验收中验证。

切换前可重复执行：

```bash
curl --resolve fast.youkeduo.shop:443:166.88.36.223 https://fast.youkeduo.shop/health
curl --resolve fast.yukeduo.shop:443:166.88.36.223 https://fast.yukeduo.shop/health
```

切换后去掉 `--resolve` 再验证，并通过 `dig +short <域名> A` 检查解析。

## 配置变更与回滚

新机的迁入源文件、迁入证书及配置备份位于 root-only 目录 `/root/shop-jump-migration/20261002/`，其中：

- `Caddyfile.before`：增加 shop 域名前的配置。
- `caddy-storage.before.tar.gz`：迁入前 Caddy 存储备份。
- `source.tar.gz` 与 `source/`：旧跳板相关 Nginx 配置、证书和私钥。
- `snapshot-start.txt`、`snapshot-end.txt`：实际采集时间（UTC）。

上述目录包含私钥，只能用于服务器内恢复，禁止复制进仓库或输出到日志。

变更前备份当前配置，完成后依次执行：

```bash
ssh sub2api-shop-vps
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
systemctl reload caddy
systemctl is-active caddy
```

随后检查两个 fast 域名和 `mogai.youkeduo.shop`。Caddy 已设置开机启动，平滑 reload 不需要重启业务容器。

若仅撤回本次增加的 shop 配置，且确认此后没有其他配置变更，可恢复 `Caddyfile.before`，再次 validate 和 reload；若已有后续修改，必须针对 shop 配置块撤回，不能整文件覆盖。DNS 已切换时，先恢复旧 DNS 入口并等待缓存过渡，再移除新机域名配置。一般不需要恢复整个 Caddy 存储，以免覆盖后续自动续期的证书。
