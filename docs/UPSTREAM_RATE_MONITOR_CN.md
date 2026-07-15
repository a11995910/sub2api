# 上游倍率监控说明

本文档说明上游倍率监控后端能力的数据结构、接口和运维注意事项。管理端“上游倍率”功能页已移除，前端不再提供配置、刷新或快照查看入口；现有后端接口和数据结构暂时保留，用于兼容已有自动化调用。

## 功能目标

上游倍率监控后端能力用于维护多个同类 Sub2API 上游站点，并通过上游登录账号读取该账号可用的分组列表和有效倍率，缓存最近一次分组倍率快照。已有自动化可以通过管理员接口读取：

- 上游站点数量和启用状态。
- 每个上游最近一次刷新状态、刷新时间和错误信息。
- 每个上游有多少个分组。
- 每个分组的文字倍率、图片倍率、订阅类型、平台、专属标记和 RPM 限制。

管理端不再展示该能力，新接入不应依赖旧页面路径。

## 数据表

新增表：

```text
upstream_rate_monitors
```

核心字段：

- `name`：站点显示名称。
- `base_url`：上游基础 URL，支持 `http` / `https` 和路径前缀，不允许 query / fragment。
- `username`：上游登录账号。
- `password_encrypted`：上游登录密码密文，使用项目现有 `SecretEncryptor` 加密。
- `enabled`：是否启用。
- `last_checked_at`：最近一次刷新时间。
- `last_status`：最近一次刷新状态，取值为 `unknown`、`success`、`failed`。
- `last_error`：最近一次刷新错误。
- `last_group_count`：最近一次成功快照中的分组数量。
- `last_snapshot`：最近一次成功读取到的分组倍率快照。

密码不会回显到前端。编辑上游时密码留空表示不修改。

## 后端接口

所有接口均为管理员接口，路径前缀为 `/api/v1/admin`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/upstream-rate-monitors` | 分页查询上游配置，支持 `search`、`enabled`、`page`、`page_size` |
| `POST` | `/upstream-rate-monitors` | 新增上游配置 |
| `GET` | `/upstream-rate-monitors/:id` | 查询单个上游 |
| `PUT` | `/upstream-rate-monitors/:id` | 更新上游配置 |
| `DELETE` | `/upstream-rate-monitors/:id` | 删除上游配置 |
| `POST` | `/upstream-rate-monitors/:id/refresh` | 立即刷新上游分组倍率快照 |

刷新流程：

1. 后端使用保存的 `base_url`、`username`、`password` 请求上游 `/api/v1/auth/login`。
2. 登录成功后取 `access_token`。
3. 使用该 token 请求上游 `/api/v1/groups/available`，读取该账号当前可用分组。
4. 再请求上游 `/api/v1/groups/rates`，读取该账号的专属分组倍率覆盖。
5. 将专属倍率覆盖到对应分组的 `rate_multiplier` 后写入 `last_snapshot`。

上游接口要求与本项目 Sub2API 普通用户接口保持兼容，至少需要返回：

- 登录接口：`data.access_token`。
- 可用分组接口：`data` 为分组数组，其中分组字段包含 `id`、`name`、`rate_multiplier` 等。
- 专属倍率接口：`data` 为 `group_id -> rate_multiplier` 映射；没有专属倍率时可以返回空对象。

注意：刷新读取的是所填登录账号实际可用的分组和有效倍率，不要求该上游账号具备管理员权限，也不会读取该上游站点的全部后台分组。

## 刷新策略

- 刷新成功：更新 `last_snapshot`、`last_group_count`、`last_checked_at`、`last_status=success`，并清空 `last_error`。
- 刷新失败：更新 `last_checked_at`、`last_status=failed`、`last_error`，保留上一份成功快照，避免页面失去历史可用数据。
- 上游返回 HTTP 2xx 但业务 `code != 0` 时按失败处理。
- 上游登录需要 2FA 时按失败处理，需要改用无需 2FA 的账号或调整上游登录策略。

## 安全限制

为避免 SSRF，`base_url` 会做以下限制：

- 仅允许 `http` 和 `https`。
- 不允许 query 和 fragment。
- 禁止 localhost、loopback、私网地址和链路本地地址。
- 请求使用安全拨号器，解析和连接阶段都会阻止私网/本机地址。

如果后续确实需要监控内网部署的上游，应单独设计允许名单配置，不要直接移除 SSRF 限制。

## 前端说明

- 管理端侧边栏不再展示“上游倍率”入口。
- 前端不再注册 `/admin/upstream-rate-monitors` 路由，也不再打包页面与 API 封装。
- 后端管理员接口暂时保留；后续删除前需要单独盘点自动化调用方和数据迁移范围。

## 上线验证

除常规上线验证外，建议回归：

```bash
curl -I https://fast.youkeduo.xyz/api/v1/admin/upstream-rate-monitors
```

由于接口需要管理员登录，上述命令未携带认证时应返回非 5xx。上线后还应确认：

- 管理端侧边栏不存在“上游倍率”菜单。
- 直接访问旧页面路径进入前端 404。
- 已有自动化在管理员鉴权下仍可调用后端接口。
- 接口详情和日志不会明文回显上游密码。
