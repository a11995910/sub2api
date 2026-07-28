# OAuth 号池透明化功能说明

## 功能目标

系统允许管理员在分组层面决定是否向有权使用该分组的用户公开 OAuth 号池状态。用户可以查看公开分组中的 OAuth 账号名称和本地缓存额度，并在自己的使用记录中核对请求实际命中的 OAuth 账号。

该功能遵循以下边界：

- 开关位于分组层面，默认关闭。
- 仅展示启用且未删除的 OAuth 账号，不展示 API Key、Setup Token、Service Account 或其他认证类型账号。
- 用户侧只读，不提供额度探测、刷新上游、额度重置、账号测试、启停或导出操作。
- 用户接口不返回账号 ID、分组 ID、凭据、备注、代理、错误详情、调度参数或原始 `extra`。
- 用户打开号池页面不会触发 OAuth 上游请求，也不会写入账号或用量数据。

## 使用角色与入口

### 管理员

管理员在“分组管理”的创建或编辑表单中配置“号池对用户可见”。管理员账号也可以从“我的账户”导航进入用户侧“号池状态”页面，查看自己有权使用的公开分组。

### 普通用户

普通用户从个人导航进入“号池状态”，路由为：

```text
/account-pool
```

用户使用记录页面包含可选的“OAuth 账号”列。只有后端返回公开账号摘要时才显示名称，否则显示 `-`。

## 分组开关

分组字段为：

```text
oauth_pool_visible
```

规则如下：

- 创建分组时默认值为 `false`。
- 编辑分组时按服务端值回显并允许更新。
- 开启后，仅有权访问该分组的用户可以查看其中符合条件的 OAuth 账号。
- 关闭后，号池页面不返回该分组，对应使用记录也不返回账号名称。
- 复制分组时继承源分组的开关值；复制出的分组仍按现有规则保持停用。
- 用户可用分组权限继续复用系统现有规则，覆盖公开分组、专属授权和有效订阅，不单独维护号池权限名单。

## 用户号池页面

页面按分组展示账号卡片。每张卡片只包含：

- OAuth 账号名称
- 5 小时额度使用率和重置时间
- 7 天额度使用率和重置时间

额度条复用 `OAuthUsageWindows.vue` 和 `UsageProgressBar.vue`。管理端账号额度区域也使用同一展示组件，但主动查询和额度重置仍由管理端组件独立负责，不会进入用户页面。

页面状态：

- 加载中：显示固定尺寸的分组和卡片骨架。
- 无可见账号：显示统一空状态，不区分“没有可见分组”和“分组中只有非 OAuth 账号”。
- 无额度快照：保留账号卡片和名称，额度区域显示“暂无额度数据”。
- 请求失败：显示统一错误信息和重试按钮，不展示内部异常详情。
- 响应式布局：移动端单列，中等屏幕双列，宽屏三列。

## 用户号池接口

认证接口：

```text
GET /api/v1/oauth-account-pool
```

接口使用现有 JWT 认证、用户模式守卫和面板限流。业务响应结构为：

```json
{
  "groups": [
    {
      "name": "公开分组",
      "accounts": [
        {
          "name": "OAuth 账号",
          "usage": {
            "five_hour": {
              "utilization": 24.5,
              "resets_at": "2026-07-28T10:00:00Z"
            },
            "seven_day": {
              "utilization": 51,
              "resets_at": "2026-08-03T10:00:00Z"
            }
          }
        }
      ]
    }
  ]
}
```

单个额度窗口没有缓存数据时返回 `null`。没有符合条件的账号时返回空 `groups`，不返回包含空账号数组的分组。

账号进入响应必须同时满足：

1. 当前用户按现有规则有权访问该分组。
2. 分组未删除、状态启用且 `oauth_pool_visible = true`。
3. 账号仍与该分组关联、未删除、状态启用且 `type = "oauth"`。

临时不可调度、限流或额度耗尽不会把启用的 OAuth 账号移出号池；永久停用或软删除账号不会返回。仓储一次批量读取所有候选账号，并只选择名称、平台、类型和额度解析所需字段。

## 额度数据来源

`AccountUsageService.BuildCachedUsage` 只根据已经加载的账号字段构建公开额度：

- OpenAI OAuth：读取账号 `extra` 中已有的 `codex_*` 快照，并复用现有额度归一化逻辑。
- Anthropic OAuth：读取现有会话窗口字段和被动采样额度快照。
- 其他平台：没有可映射快照时返回空额度。

该方法不调用 `AccountUsageService.GetUsage`，不访问 OAuth 上游，不聚合窗口费用，不写数据库。号池服务只调用该纯读取方法。

## 使用记录公开规则

普通用户使用记录列表和详情可以返回以下可选字段：

```json
{
  "oauth_account": {
    "name": "OAuth 账号"
  }
}
```

只有以下条件同时满足时才返回该摘要：

1. 使用记录属于当前登录用户。
2. `usage_logs.group_id` 对应的实际命中分组已开启号池可见。
3. `usage_logs.account_id` 对应的实际命中账号存在且类型为 OAuth。

可见性统一由 `CanExposeOAuthAccountToUser` 判断。自动分组承接场景按 `usage_logs.group_id` 记录的实际分组判断，不按 API Key 原始绑定分组判断。

普通用户 DTO 不返回 `account_id`。管理员使用记录 DTO 继续返回账号 ID 和原有最小账号摘要。列表利用已有批量关联加载，详情查询也会加载 Account 和 Group 后执行同一判断，不产生逐行查询。

账号改名后，历史记录展示当前账号名称；账号删除或关联缺失时不展示名称，但使用记录主体仍正常返回。用户 CSV 导出不包含 OAuth 账号字段，也不支持按内部账号筛选。

## 数据表与迁移

核心数据关系：

- `groups.oauth_pool_visible`：分组公开开关，`BOOLEAN NOT NULL DEFAULT FALSE`。
- `account_groups`：分组与账号关联，决定账号是否属于号池。
- `accounts`：账号名称、类型、状态和缓存额度来源。
- `usage_logs.group_id`：请求实际命中的分组。
- `usage_logs.account_id`：请求实际命中的账号。

迁移文件为：

```text
backend/migrations/192_group_oauth_pool_visible.sql
```

旧分组在迁移后保持关闭，无需数据回填。当前查询从用户可访问分组集合进入，不需要为单个布尔字段增加索引。

## 异常与兼容处理

- 未认证请求由 JWT 中间件拒绝。
- 用户权限、分组或账号状态在读取期间发生变化时，仓储查询会再次校验分组和账号条件。
- 账号同属多个公开分组时，会在每个有权访问的分组中展示；响应不会暴露关联优先级。
- 分组或账号名称重复不改变后端权限判断，前端按列表位置维持稳定渲染。
- 缓存字段缺失、格式异常或已过期不会触发上游补查；无法解析的窗口按无快照处理。
- 旧客户端不应依赖普通用户使用记录中的内部 `account_id`；该字段仅保留在管理员接口。

## 自动化验证

后端覆盖：

- 迁移默认值与可重复执行语句。
- 分组创建、更新和复制的开关传递。
- 用户可访问分组过滤和空分组省略。
- PostgreSQL 中分组账号联表查询、公开分组过滤和稳定排序，避免邻表字段产生歧义。
- OpenAI 与 Anthropic 缓存额度构建。
- 普通用户账号 ID 脱敏、OAuth 名称条件展示和管理员字段保留。
- 服务、处理器、仓储、路由和迁移相关包回归。

前端覆盖：

- 共享额度组件的基础窗口、扩展窗口、统计开关和空额度状态。
- 号池页面成功、空数据、失败重试状态。
- 用户使用记录 OAuth 账号列和 CSV 不含账号名称。
- 管理端分组创建提交、编辑回显与更新提交。
- 管理端账号额度组件回归。

验证命令：

```bash
cd backend
go test ./internal/service ./internal/handler/... ./internal/repository ./internal/server/routes ./migrations
go test -tags integration ./internal/repository -run TestAccountRepoSuite/TestListActiveOAuthByGroupIDs_OrdersWithoutAmbiguousColumns -count=1

cd ../frontend
npm run typecheck
npm run test:run -- src/components/account/__tests__/OAuthUsageWindows.spec.ts src/views/user/__tests__/OAuthAccountPoolView.spec.ts src/views/user/__tests__/UsageView.spec.ts src/components/admin/usage/__tests__/UsageTable.spec.ts src/views/admin/__tests__/GroupsView.autoFallback.spec.ts src/views/admin/__tests__/GroupsView.duplicate.spec.ts src/components/account/__tests__/AccountUsageCell.spec.ts
npm run build
```

## 发布验收

功能代码必须先提交并推送到 `main`，再由正式 VPS 的隔离 staging 从同一 `origin/main` commit 构建。staging 应使用同时关联 OAuth 与 API Key 账号的测试分组验证：

1. 开关关闭时号池页面不出现该分组，使用记录不显示账号。
2. 开关开启后只出现 OAuth 账号，不出现 API Key 账号。
3. 用户额度条与管理端相同缓存的显示结果一致。
4. 用户访问页面不产生 OAuth 上游探测、额度重置或账号写入。
5. OAuth 和 API Key 请求分别命中后，使用记录只对 OAuth 展示账号名称。
6. 用户使用记录 CSV 不包含账号名称。

prod 只能切换到 staging 已验证的同一 `main` commit，并等待用户明确口头确认。
