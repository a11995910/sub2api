# 上游倍率监控说明

本文档说明后台“上游倍率监控”菜单的用途、数据结构、接口和运维注意事项。

## 功能目标

上游倍率监控用于维护多个同类 Sub2API 上游站点，并通过上游登录账号读取该账号可用的分组列表和有效倍率，缓存最近一次分组倍率快照。管理员可以在后台实时查看：

- 上游站点数量和启用状态。
- 每个上游最近一次刷新状态、刷新时间和错误信息。
- 每个上游有多少个分组。
- 每个分组的文字倍率、图片倍率、订阅类型、平台、专属标记和 RPM 限制。

后台入口：

```text
/admin/upstream-rate-monitors
```

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

后台页面提供：

- 搜索：按名称、URL、账号搜索。
- 状态过滤：全部、仅启用、仅禁用。
- 添加/编辑上游：支持名称、URL、账号、密码和启用开关。
- 单站刷新：立即读取指定上游账号可用分组和有效倍率。
- 当前页刷新：依次刷新当前页已启用且密码可解密的上游。
- 倍率快照弹窗：展示分组明细和最近错误。

## 上线验证

除常规上线验证外，建议回归：

```bash
curl -I https://fast.youkeduo.xyz/admin/upstream-rate-monitors
curl -I https://fast.youkeduo.xyz/api/v1/admin/upstream-rate-monitors
```

由于接口需要管理员登录，上述第二个命令未携带认证时应返回非 5xx。上线后还应通过浏览器登录管理端确认：

- 菜单“上游倍率”能正常显示。
- 页面能打开且不出现前端报错。
- 新增上游时密码不会在详情和列表中明文回显。
- 刷新失败时页面展示错误并保留旧快照。
