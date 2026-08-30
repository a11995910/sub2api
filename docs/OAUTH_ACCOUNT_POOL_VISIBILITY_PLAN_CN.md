# OAuth 号池透明化功能说明

## 功能目标

系统允许管理员在分组层面决定是否向有权使用该分组的用户公开 OAuth 号池状态。用户可以查看公开分组中的账号标识、套餐、实时连接数、额度窗口和账号过期时间，并可在自己的使用记录中核对请求实际命中的 OAuth 账号。号池页面和接口不公开请求次数或 Token 用量。

功能边界如下：

- 开关位于分组层面，默认关闭。
- 仅展示启用且未删除的 OAuth 账号，不展示 API Key、Setup Token、Service Account 或其他认证类型账号。
- 用户侧只读，不提供上游额度探测、额度重置、账号测试、启停、编辑或导出操作。
- 用户接口不返回账号 ID、分组 ID、管理员自定义账号名称、凭据、备注、代理、错误详情、调度参数或原始 `extra`。
- 用户打开号池页面不会访问 OAuth 上游，也不会写入账号或用量数据。

## 使用角色与入口

### 管理员

管理员在“分组管理”的创建或编辑表单中配置“号池对用户可见”。管理员账号也可以从“我的账户”导航进入用户侧号池页面，查看自己有权使用的公开分组。

### 普通用户

普通用户从个人导航进入“号池状态”，路由为：

```text
/account-pool
```

用户使用记录页面包含可选的“OAuth 账号”列。只有后端安全解析出真实账号标识时才显示该标识，否则显示 `-`；该列不回退管理员自定义账号名称。

## 分组开关与权限

分组字段为：

```text
oauth_pool_visible
```

规则如下：

- 创建分组时默认值为 `false`。
- 编辑分组时按服务端值回显并允许更新。
- 开启后，仅有权访问该分组的用户可以查看其中符合条件的 OAuth 账号。
- 关闭后，号池页面不返回该分组，对应使用记录也不返回账号标识。
- 复制分组时继承源分组的开关值；复制出的分组仍按现有规则保持停用。
- 用户可用分组权限复用系统现有规则，覆盖公开分组、专属授权和有效订阅，不单独维护号池权限名单。

账号进入响应必须同时满足：

1. 当前用户按现有规则有权访问该分组。
2. 分组未删除、状态启用且 `oauth_pool_visible = true`。
3. 账号仍与该分组关联、未删除、状态启用且 `type = "oauth"`。

临时不可调度、限流、并发占满或额度耗尽不会把启用的 OAuth 账号移出号池；永久停用或软删除账号不会返回。

## 用户号池页面

页面按分组展示账号卡片。每张卡片包含：

- 脱敏账号标识。
- 套餐标签。
- 当前连接数与账号并发总数，格式为 `当前值 / 总数`。
- 根据实时连接数显示“可用”“使用中”或“繁忙”状态。
- 5 小时和 7 天额度使用率、重置时间。
- 账号过期时间；未设置时显示“永久有效”。

页面顶部显示可见分组数和全部可见号池的唯一账号数。同一账号属于多个可见分组时，全局账号数只计算一次。页面不显示全局、分组或账号级请求/Token 统计。

实时连接徽标复用 `AccountConcurrencyBadge.vue`，状态颜色按负载区分：可用为绿色、使用中为蓝色、繁忙为黄色。额度条复用 `OAuthUsageWindows.vue` 和 `UsageProgressBar.vue`，在号池卡片内启用全宽轨道。

页面状态：

- 加载中：显示与汇总、分组和账号卡片尺寸匹配的骨架。
- 无可见账号：显示统一空状态，不区分“没有可见分组”和“分组中只有非 OAuth 账号”。
- 无账号标识：显示“账号信息不可用”，不使用自定义账号名称兜底。
- 无套餐：显示“未知套餐”。
- 无额度快照：仍显示状态、并发和过期时间，额度区域显示“暂无额度数据”。
- 请求失败：显示统一错误信息和重试按钮，不展示内部异常详情。
- 响应式布局：移动端单列，中等屏幕双列，宽屏按三至四列紧凑网格展示；长邮箱允许换行且不覆盖套餐或连接徽标。

## 用户号池接口

认证接口：

```text
GET /api/v1/oauth-account-pool
```

接口使用现有 JWT 认证、用户模式守卫和面板限流。管理员可以看到完整账号标识，普通用户看到脱敏标识；其余响应结构一致。接口不返回 `stats`、`requests` 或 `tokens`，`usage` 仅包含额度百分比和重置时间：

```json
{
  "groups": [
    {
      "name": "公开分组",
      "accounts": [
        {
          "identifier": "owner@example.com",
          "plan_type": "Pro 20x",
          "current_concurrency": 6,
          "concurrency": 15,
          "expires_at": "2026-09-30T12:00:00Z",
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
      ],
      "summary": { "account_count": 1 }
    }
  ],
  "summary": { "account_count": 1 }
}
```

单个额度窗口没有缓存数据时返回 `null`。账号未设置过期时间时 `expires_at` 返回 `null`。没有符合条件的账号时返回空 `groups` 和零值 `summary`，不返回包含空账号数组的分组。

## 真实账号与套餐来源

真实账号标识只从以下安全字段按顺序解析：

1. `accounts.extra.email_address`
2. `accounts.extra.email`
3. `accounts.credentials.email`
4. 影子账号对应母账号按相同规则解析出的标识

解析结果只下发字符串，不下发 `credentials` 或 `extra`。解析不到时保持空值，不允许使用 `accounts.name` 兜底。

套餐读取账号自身 `accounts.credentials.plan_type`；影子账号没有自身套餐时读取母账号的 `plan_type`。已知展示映射为：

- `pro`、`chatgptpro`：`Pro 20x`
- `team`：`Team`
- `plus`：`Plus`
- `k12`、`chatgptk12`：`K12`
- `free`、`basic`：`Free`

匹配时忽略空格、下划线、连字符和大小写。未知非空套餐原样展示，避免把新的上游套餐误判为现有套餐。该映射只影响显示，不改变调度、计费或额度规则。

## 额度、过期时间与连接数据

### 额度快照

`AccountUsageService.BuildCachedUsage` 只根据已经加载的账号字段构建公开额度：

- OpenAI OAuth：读取账号 `extra` 中已有的 `codex_*` 快照，并复用现有额度归一化逻辑。
- Anthropic OAuth：读取现有会话窗口字段和被动采样额度快照。
- 其他平台：没有可映射快照时返回空额度。

号池服务不调用可能访问 OAuth 上游的 `AccountUsageService.GetUsage`。

### 账号过期时间

账号过期时间优先读取与套餐对应的 `accounts.credentials.subscription_expires_at`；没有订阅日期时回退 `accounts.expires_at`。影子账号复用母账号按同一规则解析的时间。OAuth Access Token 的 `expires_at` 不参与展示，避免把短期 Token 刷新时间误当成账号或套餐到期时间。两个来源均为空时显示“永久有效”。号池查询只读取已存储字段，不访问 OAuth 上游，也不更新账号状态。

号池服务不读取 `usage_logs`，也不计算或返回 5 小时、7 天及累计请求/Token 统计。

### 当前连接与并发总数

当前连接数复用 `ConcurrencyService.GetAccountConcurrencyBatch`，读取 Redis 中的实时账号并发槽位；并发总数读取 `accounts.concurrency`。查询按唯一账号 ID 一次批量完成。Redis 查询失败时沿用管理端账号列表的降级规则，当前值显示为 `0`，不影响其他号池数据返回。

## 使用记录公开规则

普通用户使用记录列表和详情可以返回以下可选字段：

```json
{
  "oauth_account": {
    "identifier": "owner@example.com"
  }
}
```

只有以下条件同时满足时才允许返回该摘要：

1. 使用记录属于当前登录用户。
2. `usage_logs.group_id` 对应的实际命中分组已开启号池可见。
3. `usage_logs.account_id` 对应的实际命中账号存在且类型为 OAuth。
4. 当前账号或影子母账号可以解析出安全的真实账号标识。

可见性统一由 `CanExposeOAuthAccountToUser` 判断。自动分组承接场景按 `usage_logs.group_id` 记录的实际分组判断，不按 API Key 原始绑定分组判断。

普通用户 DTO 不返回 `account_id` 或管理员自定义账号名称。管理员使用记录 DTO 继续返回账号 ID 和原有最小账号摘要。列表利用已有批量关联加载，影子账号母账号也通过批量关联加载，不产生逐行查询。

账号邮箱变化后，历史记录展示当前真实账号标识；账号删除、关联缺失或标识缺失时显示 `-`，使用记录主体仍正常返回。用户 CSV 导出不包含 OAuth 账号字段，也不支持按内部账号筛选。

## 数据表与迁移

核心数据关系：

- `groups.oauth_pool_visible`：分组公开开关，`BOOLEAN NOT NULL DEFAULT FALSE`。
- `account_groups`：分组与账号关联，决定账号是否属于号池。
- `accounts`：账号类型、状态、真实标识来源、套餐、并发总数、账号过期时间和缓存额度来源。
- `usage_logs.group_id`：请求实际命中的分组。
- `usage_logs.account_id`：请求实际命中的账号。
- Redis 账号并发槽位：当前活跃连接数来源。

分组开关迁移文件为：

```text
backend/migrations/192_group_oauth_pool_visible.sql
```

旧分组在迁移后保持关闭，无需数据回填。本功能的统计、套餐和连接字段来自已有数据结构，不增加数据库字段。

## 异常与兼容处理

- 未认证请求由 JWT 中间件拒绝。
- 用户权限、分组或账号状态在读取期间发生变化时，仓储查询会再次校验分组和账号条件。
- 账号同属多个公开分组时，会在每个有权访问的分组中展示；响应不会暴露关联优先级。
- 分组名称或真实账号标识重复不改变后端权限判断。
- 缓存额度字段缺失、格式异常或已过期不会触发上游补查。
- 统计查询失败会让号池接口返回统一错误，避免把查询失败误报为零用量。
- 实时连接查询失败仅降级当前连接数，不影响真实账号、套餐、统计和额度返回。
- 旧客户端不应依赖普通用户使用记录中的 `oauth_account.name`；当前字段为 `oauth_account.identifier`。

## 自动化验证

后端覆盖：

- 分组创建、更新、复制和用户可访问分组过滤。
- 号池仓储公开分组过滤、稳定排序、真实字段选择和影子母账号回退。
- 真实账号绝不回退管理员自定义名称。
- Pro 20x、Team、Plus、K12、Free 及未知套餐展示规则。
- OpenAI 与 Anthropic 缓存额度构建。
- 每账号不同窗口起点的 5h/7d/累计批量统计。
- 实时连接批量读取及并发总数返回。
- 普通用户账号 ID 脱敏、真实标识条件展示和管理员字段保留。

前端覆盖：

- 号池页面真实账号、套餐、连接徽标、窗口统计、累计统计和两级汇总。
- 号池页面成功、空数据、失败重试和无额度状态。
- 用户使用记录只显示真实账号标识，CSV 不含账号字段。
- 管理端账号连接徽标复用回归。
- 分组开关和管理端账号额度组件回归。

验证命令：

```bash
cd backend
GOCACHE=/tmp/sub2api-go-cache go test ./internal/service ./internal/handler/... ./internal/repository ./internal/server/routes ./migrations
GOCACHE=/tmp/sub2api-go-cache go test -tags integration ./internal/repository -run 'Test(Account|UsageLog)RepoSuite' -count=1

cd ../frontend
npm run typecheck
npm run test:run -- src/views/user/__tests__/OAuthAccountPoolView.spec.ts src/views/user/__tests__/UsageView.spec.ts src/components/admin/usage/__tests__/UsageTable.spec.ts
npm run build
```

## 发布验收

功能代码必须先提交并推送到 `main`，再由正式 VPS 的隔离 staging 从同一 `origin/main` commit 构建。staging 使用同时关联 OAuth 与 API Key 账号的测试分组验证：

1. 开关关闭时号池页面不出现该分组，使用记录不显示账号。
2. 开关开启后只出现 OAuth 账号，不出现 API Key 账号。
3. 账号卡片显示真实邮箱、正确套餐以及与管理端一致的当前连接数/并发总数。
4. 用户 5h/7d 请求和 Token 与管理端相同窗口统计一致，累计及分组/全局汇总计算正确。
5. 用户额度条与管理端相同缓存的显示结果一致。
6. 用户访问页面不产生 OAuth 上游探测、额度重置或账号写入。
7. OAuth 和 API Key 请求分别命中后，使用记录只对 OAuth 展示真实账号，不出现管理员自定义名称。
8. 用户使用记录 CSV 不包含 OAuth 账号字段。

prod 只能切换到 staging 已验证的同一 `main` commit，并等待用户明确口头确认。
