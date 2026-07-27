# 可见号池模型可见性 Implementation Plan

> **供自动化执行者：** 本计划必须在当前会话内按复选框逐项执行；本任务未授权子代理，不得分派子任务。

**目标：** 让模型广场和模型测试台只把模型关联到持久账号池实际支持的用户可见分组，同时避免临时限流或过载导致页面模型闪烁。

**架构：** `GET /api/v1/channels/available` 继续以渠道定价作为候选模型来源，但为每个模型增加 `group_ids`。Handler 通过 `AccountRepository.ListModelAvailabilityCandidates` 按分组读取 `active + schedulable` 账号，并用 `Account.IsModelSupported` 计算支持关系；前端在现有模型类型过滤之后再与 `group_ids` 求交集，旧后端缺少该字段时沿用原行为。

**技术栈：** Go、Gin、Ent、Google Wire、Vue 3、TypeScript、Vitest。

## 全局约束

- 只使用账号持久配置；忽略限流、过载、临时不可调度、过期窗口和运行时阻断。
- 用户响应不得包含账号 ID、账号名称、账号类型、OAuth 信息、凭据或任何密钥。
- 保留当前脏工作区和 `stash@{0}`，所有修改仅发生在 `.worktrees/visible-account-pool`。
- 代码、注释、文档和提交说明使用中文。
- staging 验证通过后停止，未经用户明确确认不得合并 `main` 或切换 prod。

---

### Task 1: 后端持久号池可见性契约

**文件：**
- 修改：`backend/internal/handler/available_channel_handler.go`
- 修改：`backend/internal/handler/available_channel_handler_test.go`
- 修改：`backend/cmd/server/wire_gen.go`

**接口：**
- 使用：`AccountRepository.ListModelAvailabilityCandidates(ctx, groupID, platforms, false) ([]Account, error)`。
- 产出：`userSupportedModel.GroupIDs []int64`，JSON 字段为 `group_ids`。

- [x] **Step 1: 编写失败测试**

```go
func TestAnnotateSupportedModelGroups_UsesPersistentAccountPool(t *testing.T) {
	models := []userSupportedModel{{Name: "gpt-5.6", Platform: service.PlatformOpenAI, Kind: modelKindToken}}
	groups := []userAvailableGroup{{ID: 74, Platform: service.PlatformOpenAI}, {ID: 75, Platform: service.PlatformOpenAI}}
	accounts := map[int64][]service.Account{
		74: {{Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.6": "gpt-5.6"}}}},
		75: {{Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
	}

	got := annotateSupportedModelGroups(models, groups, accounts)
	require.Equal(t, []int64{74}, got[0].GroupIDs)
}
```

- [x] **Step 2: 运行测试并确认失败**

运行：`cd backend && GOCACHE=/tmp/sub2api-go-cache GOMODCACHE=/tmp/sub2api-go-modcache go test -tags=unit ./internal/handler -run 'TestAnnotateSupportedModelGroups|TestUserSupportedModel'`

预期：因 `GroupIDs` 或 `annotateSupportedModelGroups` 尚不存在而编译失败。

- [x] **Step 3: 实现最小后端逻辑**

```go
type userSupportedModel struct {
	Name     string                     `json:"name"`
	Platform string                     `json:"platform"`
	Kind     string                     `json:"kind"`
	Pricing  *userSupportedModelPricing `json:"pricing"`
	GroupIDs []int64                    `json:"group_ids"`
}

func annotateSupportedModelGroups(
	models []userSupportedModel,
	groups []userAvailableGroup,
	accountsByGroup map[int64][]service.Account,
) []userSupportedModel {
	out := make([]userSupportedModel, 0, len(models))
	for _, model := range models {
		for _, group := range filterGroupsForModelKind(groups, model.Kind) {
			for i := range accountsByGroup[group.ID] {
				if accountsByGroup[group.ID][i].IsModelSupported(model.Name) {
					model.GroupIDs = append(model.GroupIDs, group.ID)
					break
				}
			}
		}
		if len(model.GroupIDs) > 0 {
			out = append(out, model)
		}
	}
	return out
}
```

Handler 在构造函数中接收 `service.AccountRepository`，按用户可见分组调用 `ListModelAvailabilityCandidates`；查询失败直接返回服务端错误，不回退到不准确的笛卡尔组合。Wire 生成文件同步传入已有 `accountRepository`。

- [x] **Step 4: 运行后端测试**

运行：`cd backend && GOCACHE=/tmp/sub2api-go-cache GOMODCACHE=/tmp/sub2api-go-modcache go test -tags=unit ./internal/handler ./internal/repository ./internal/service`

预期：全部通过。

- [x] **Step 5: 将后端契约纳入本次原子提交**

后端契约与对应前端消费、测试和文档使用同一个可回溯提交，避免接口两侧版本分离。

### Task 2: 前端按可见分组消费 `group_ids`

**文件：**
- 修改：`frontend/src/api/channels.ts`
- 修改：`frontend/src/utils/modelKind.ts`
- 修改：`frontend/src/utils/__tests__/modelKind.spec.ts`
- 修改：`frontend/src/views/user/ModelMarketView.vue`
- 修改：`frontend/src/views/user/ModelTestView.vue`
- 修改：`frontend/src/views/user/__tests__/ModelMarketView.spec.ts`
- 修改：`frontend/src/views/user/__tests__/ModelTestView.spec.ts`

**接口：**
- 使用：`UserSupportedModel.group_ids?: number[]`。
- 产出：`filterGroupsByModelAvailability(groups, model)`，先按模型类型过滤，再按 `group_ids` 求交集。

- [x] **Step 1: 编写失败测试**

```ts
it('只保留模型持久号池支持的分组', () => {
  const groups = [
    { ...baseGroup, id: 74 },
    { ...baseGroup, id: 75 },
  ]
  expect(filterGroupsByModelAvailability(groups, {
    name: 'gpt-5.6',
    kind: 'token',
    pricing: null,
    group_ids: [74],
  }).map((group) => group.id)).toEqual([74])
})
```

另在 `ModelMarketView.spec.ts` 和 `ModelTestView.spec.ts` 构造同平台两个分组，断言模型只出现在 `group_ids` 指定的分组中。

- [x] **Step 2: 运行测试并确认失败**

运行：`cd frontend && pnpm test:run src/utils/__tests__/modelKind.spec.ts src/views/user/__tests__/ModelMarketView.spec.ts src/views/user/__tests__/ModelTestView.spec.ts`

预期：因 `filterGroupsByModelAvailability` 尚不存在而失败。

- [x] **Step 3: 实现兼容过滤**

```ts
export function filterGroupsByModelAvailability(
  groups: UserAvailableGroup[] | undefined,
  model: Pick<UserSupportedModel, 'kind' | 'name' | 'pricing' | 'group_ids'>,
): UserAvailableGroup[] {
  const kind = resolveModelKind(model)
  const eligible = filterGroupsByModelKind(groups, kind)
  if (!Array.isArray(model.group_ids)) return eligible
  const visible = new Set(model.group_ids)
  return eligible.filter((group) => visible.has(group.id))
}
```

模型广场与模型测试台都改用该函数。`group_ids` 缺失时保留旧后端行为；显式空数组表示该模型没有可见分组。

- [x] **Step 4: 运行前端测试和类型检查**

运行：`cd frontend && pnpm test:run src/utils/__tests__/modelKind.spec.ts src/views/user/__tests__/ModelMarketView.spec.ts src/views/user/__tests__/ModelTestView.spec.ts`

运行：`cd frontend && pnpm typecheck`

预期：全部通过。

### Task 3: 文档、完整门禁与 staging

**文件：**
- 修改：`docs/AVAILABLE_CHANNELS_CN.md`
- 修改：`docs/RECHARGE_MODEL_MARKET_CN.md`
- 创建：`docs/superpowers/plans/2026-07-27-visible-account-pool.md`

**接口：**
- 记录：`supported_models[].group_ids` 的语义、持久状态边界和敏感字段边界。
- 记录：生产 `pro正价分组` 只作为 staging 数据样本，不在仓库写入账号详情或凭据。

- [x] **Step 1: 同步当前系统文档**

文档明确：模型候选仍来自渠道配置；`group_ids` 只表示用户可见且持久账号池支持的分组；临时限流、过载、临时不可调度和过期窗口不影响页面展示；不返回账号身份或认证信息。

- [x] **Step 2: 运行完整质量门禁**

运行：`PATH="$(go env GOPATH)/bin:$PATH" make test`

运行：`make build`

运行：`make secret-scan`

预期：测试、构建和敏感信息扫描全部通过。

执行时同时修复了最新 `origin/dev` 阻断完整门禁的 5 个基线 lint，以及两处因新增 `PanelRateLimiter` 参数未同步导致的路由测试编译问题；修复均为格式、弃用字段替换、TLS 测试最低版本和无效初值清理，不改变业务语义。

- [x] **Step 3: 检查并提交剩余修改**

```bash
git status --short
git diff --check
git diff --stat
git add backend frontend/src docs/AVAILABLE_CHANNELS_CN.md docs/RECHARGE_MODEL_MARKET_CN.md
git add -f docs/superpowers/plans/2026-07-27-visible-account-pool.md
git commit -m "feat: 按可见号池过滤模型分组"
```

- [ ] **Step 4: 推送开发分支并部署隔离 staging**

将已验证提交快进推送到 `origin/dev`。正式 VPS 在 `/opt/sub2api/repo` 拉取同一提交，构建 `sub2api:staging-<commit>`，使用独立 staging compose、数据库、Redis、数据目录和 `18080` 端口启动。

- [ ] **Step 5: 使用生产配置样本验收**

只读验证 `pro正价分组` 返回的模型 `group_ids` 与其 active、schedulable OAuth 账号映射一致；切换临时限流/过载状态不应改变列表。核对健康检查、`/api/v1/channels/available`、模型广场、模型测试台和容器错误日志。staging 验证完成后报告结果并等待用户确认，不切换 prod。

## 自检结果

- 需求覆盖：后端数据源、DTO、前端两个消费者、兼容策略、文档、门禁和 staging 均有明确任务。
- 占位符检查：无 `TBD`、`TODO`、`implement later` 或未定义接口。
- 类型一致性：后端 `group_ids` 为 `[]int64`；前端为可选 `number[]`；过滤函数同时接受模型类型和可见分组。
