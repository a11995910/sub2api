# Video Test Timeout 15m Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将模型测试台视频生成默认等待时间从 5 分钟提高到 15 分钟。

**Architecture:** 保持现有前端轮询结构，只调整 `testVideoGeneration` 的默认超时常量。通过模拟 `Date.now()` 验证 5 分钟后继续轮询、15 分钟后才抛出超时。

**Tech Stack:** Vue 3、TypeScript、Vitest、pnpm。

## Global Constraints

- 默认超时必须为 `15 * 60 * 1000` 毫秒。
- 显式传入 `timeoutMs` 时保持现有行为。
- 不修改视频网关、计费或任务绑定逻辑。

---

### Task 1: 提高视频测试默认超时

**Files:**
- Modify: `frontend/src/api/modelTest.ts:239`
- Test: `frontend/src/api/__tests__/modelTest.spec.ts`

**Interfaces:**
- Consumes: `VideoGenerationTestRequest.timeoutMs?: number`
- Produces: `testVideoGeneration(req): Promise<VideoGenerationTestResult>` 的 15 分钟默认等待行为

- [ ] **Step 1: 写失败测试**

新增用例，模拟时间从 `0` 推进到 `5 * 60 * 1000 + 1`，确认仍发起状态轮询；再推进到 `15 * 60 * 1000 + 1`，确认抛出包含任务 ID 的 `408`。

- [ ] **Step 2: 验证测试失败**

Run: `pnpm exec vitest run src/api/__tests__/modelTest.spec.ts`

Expected: 新用例在 5 分钟后按旧默认值提前抛出超时。

- [ ] **Step 3: 写最小实现**

```ts
const timeoutMs = Math.max(1000, req.timeoutMs ?? 15 * 60 * 1000)
```

- [ ] **Step 4: 验证测试和构建**

Run: `pnpm exec vitest run src/api/__tests__/modelTest.spec.ts`

Expected: 全部通过。

Run: `pnpm type-check && pnpm build`

Expected: 两条命令退出码均为 0。

- [ ] **Step 5: 提交**

```bash
git add docs/superpowers/specs/2026-07-19-video-test-timeout-15m-design.md \
  docs/superpowers/plans/2026-07-19-video-test-timeout-15m.md \
  frontend/src/api/modelTest.ts \
  frontend/src/api/__tests__/modelTest.spec.ts
git commit -m "fix: 延长视频测试默认等待时间"
```
