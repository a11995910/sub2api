# 画布与账号管理运行说明

Infinite Canvas 和 Team Manage 均运行于新正式 VPS `185.61.210.32`，通过 `ssh sub2api-migration-vps` 管理。两个应用使用独立的 SQLite 数据库，与 Sub2API 的 PostgreSQL、Redis 和发布流程分开维护。

## Infinite Canvas

- 运行容器：`infinite-canvas`，Compose project 为 `db2d581-image-fix`。
- 发布目录：`/opt/infinite-canvas-releases/db2d581-image-fix`，其中 `docker-compose.yml`、`.env`、`data/` 分别保存编排、运行配置和持久数据。
- 镜像：`infinite-canvas:local`，迁入并验证的镜像 ID 为 `sha256:220514e7868be49260b5744591812106b0f61eea137e044da9eb1fd79d6deea2`。迁移沿用旧机实际运行镜像，没有重新构建；版本核实以镜像 ID 为准。
- 宿主机仅监听 `127.0.0.1:13000`，映射容器 `3000`；Nginx 的画布上游固定为该宿主机端口，不使用容器动态 IP。
- 对外入口：`https://canvas.youkeduo.shop` 和 `https://canvas.youkeduo.xyz`。shop 经跳板转发，xyz 当前直接解析到新正式机。
- 数据库：发布目录下的 `data/infinite-canvas.db`；生成图片位于 `data/generated-images/`。
- 健康接口：`http://127.0.0.1:13000/api/health`，成功时返回 `ok`。容器重启策略为 `unless-stopped`。

检查和启动必须使用现有 Compose 配置及已验证镜像，不在迁移恢复过程中隐式构建其他版本：

```bash
cd /opt/infinite-canvas-releases/db2d581-image-fix
docker compose ps
docker compose up -d --no-build
curl --fail http://127.0.0.1:13000/api/health
```

## Team Manage

- systemd 服务：`team-manage.service`，已启用开机启动，以 `team-manage` 用户运行。
- 当前发布：`/opt/team-manage/current` 指向 `/opt/team-manage/releases/20260929-daa1505`。
- Python 环境：该发布的 `.venv` 指向 `/opt/team-manage/releases/20260917-181400/.venv`。保留此依赖目录，父目录必须允许服务用户遍历。
- 运行配置：`/opt/team-manage/shared/.env`，由 systemd 加载，保持 `root:root`、`0600`；数据目录由新机的 `team-manage` 用户及组持有。
- 数据库：`/opt/team-manage/shared/data/team_manage.db`。
- 监听地址：`0.0.0.0:8008`。访问 `http://185.61.210.32:8008` 会进入现有管理流程；登录入口为 `/aury-gate-x83p`，管理页面为 `/admin/`。
- 健康接口：`http://127.0.0.1:8008/health`。
- Sub2API 连接仍使用 `https://fast.youkeduo.shop`，保留既有账号、分组映射、加密密钥和登录凭据。

服务启动会执行账号恢复、同步和自动化任务，同一业务数据库不得在两台机器上同时启动应用。新机原有的 `migration-isolation.conf` 已归档，不再启用 `PrivateNetwork=true`。

```bash
systemctl is-active team-manage
systemctl is-enabled team-manage
curl --fail http://127.0.0.1:8008/health
```

## 旧入口和停机边界

旧正式 VPS `205.185.113.15` 的画布容器已停止，重启策略为 `no`；Team Manage 已停止并取消开机启动。旧数据保留作恢复参考，不再作为运行数据源。

旧机 Nginx 继续提供入口兼容：

- 旧画布域名及 `13080` 入口通过 HTTPS 转发至 `185.61.210.32:443`，启用 SNI、证书链验证和深度为 3 的证书链校验。
- 旧账号管理 `8008` 入口由 `/etc/nginx/conf.d/team-manage-migrated.conf` 转发至 `185.61.210.32:8008`，原应用不会占用该端口。

shop DNS 当前仍有 `207.57.145.15` 和 `192.220.36.75` 两条 A 记录。两个现有入口及新跳板 `166.88.36.223` 均已验证可以访问新机画布；DNS 调整仍按 [shop 跳板运行说明](SHOP_JUMP_CN.md) 由用户处理。

旧机还运行 Excel Bridge，并承担上述旧入口转发。关闭旧机前必须另外处理这些依赖。

## 数据一致性与恢复

本次迁移的停写起点为北京时间 **2026-10-02 09:52:56**，两个源应用于 **09:53:03** 确认停止，最终快照于 **09:53:11** 传输完成。应用停止后的完整数据目录是最终边界，包含新增、修改、删除和当前状态，未按创建时间筛选记录。

新机 root-only 恢复目录为 `/root/aux-app-migration/20261002T014906Z/`，保留迁入前配置、原始应用及依赖归档、画布镜像、最终数据归档、源与目标校验清单、实际时间和验收回执。SQLite 主库执行 `integrity_check`；逐文件哈希校验排除可重建的共享内存文件，且确认停写后的 WAL 为空。

最终数据边界包含：

| 数据 | 数量 |
| --- | ---: |
| 画布用户 | 503 |
| 画布工作流 | 54 |
| 画布提示词 | 1748 |
| 账号管理全部账号记录（含软删除） | 86 |
| 账号管理未软删除账号 | 49 |
| Team 记录 | 2 |
| 母账号记录 | 3 |

验收覆盖数据库完整性、40 个持久文件哈希、195 个加密字段解密、两个应用的管理员登录与列表读取、画布静态资源、健康接口以及新旧公网入口。未通过实际付费生成或账号轮换操作验证业务。

回滚必须先判断新机是否已产生写入。当前新机已接流，不能直接重启旧机的冻结数据库：应先停新应用，将最新 SQLite 与文件完整同步回目标恢复位置，校验完整性、权限和加密配置，再切换入口。回切旧 Team Manage 前，还必须移除旧机 Nginx 对 `8008` 的转发监听并执行 `nginx -t`、reload，避免端口冲突。整个过程只允许一侧应用运行。

恢复 Nginx 时仅恢复相关应用的配置块，保留迁移之后其他业务的修改；新旧配置备份均位于新机恢复目录。不要整目录覆盖当前配置，也不要恢复或重建 Sub2API 数据库。临时传输 SSH 授权已撤销，对应私钥已删除。
