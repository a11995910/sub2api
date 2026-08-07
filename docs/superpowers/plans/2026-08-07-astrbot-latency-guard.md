# AstrBot Latency Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增一个独立的 AstrBot 插件，读取 Sub2API 使用记录中的 `first_token_ms`，按同级账号计算延迟健康度，通过飞书建议或受控修改账号 `priority`，不改动现有版本监控插件和 Sub2API 网关代码。

**Architecture:** 插件由纯函数评分策略、Sub2API 管理 API 客户端、状态持久化、飞书通知/按钮门禁和可选 AI 说明组成。规则代码决定样本、基线、惩罚和切换建议；AI 只能解释规则结果，不能直接决定或越权写入。默认 `confirm` 模式，人工确认后才修改。

**Tech Stack:** Python 3.10+, AstrBot plugin API, aiohttp, Lark card/message API, pytest/stdlib unit tests。

---

### Task 1: 建立纯函数延迟评分策略

**Files:**
- Create: `astrbot/sub2api_latency_guard/policy.py`
- Test: `tests/test_astrbot_latency_policy.py`

- [ ] **Step 1: Write failing tests for sample filtering and score states**

测试覆盖：忽略空值和非正数；样本少于 10 条不降级；慢请求比例、p50、p95、波动系数共同产生惩罚；样本数达到 30 后置信度为 1；恢复状态会清除惩罚。

```python
def test_insufficient_samples_do_not_penalize():
    result = evaluate_account_latency([1000, 5000, 9000], baseline_ms=1000)
    assert result.penalty == 0
    assert result.state == "observe"

def test_sustained_slow_samples_penalize():
    samples = [4200] * 15 + [1800] * 15
    result = evaluate_account_latency(samples, baseline_ms=2000)
    assert result.penalty >= 5
    assert result.state in {"degraded", "slow"}

def test_single_spike_is_not_a_slow_account():
    samples = [2000] * 29 + [60000]
    result = evaluate_account_latency(samples, baseline_ms=2000)
    assert result.penalty <= 2

def test_recovery_requires_normal_samples():
    result = evaluate_account_latency([1800] * 30, baseline_ms=2000, previous_penalty=8)
    assert result.penalty == 0
    assert result.state == "healthy"
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run: `python -m pytest tests/test_astrbot_latency_policy.py -q`

Expected: FAIL because `astrbot.sub2api_latency_guard.policy` does not exist.

- [ ] **Step 3: Implement deterministic policy types and calculations**

`policy.py` 定义 `LatencyEvaluation` 数据类和 `evaluate_account_latency()`：过滤异常样本；样本数 `<10` 返回 `observe`；计算 p50、p95、EWMA、慢请求比例和 `p95/p50`；使用 `confidence=min(1,n/30)` 缩放惩罚；惩罚限定为 0、2、5、10；输入延迟超过 60 秒按 60 秒计算；不负责 HTTP、AI 或写账号。

- [ ] **Step 4: Run focused tests and verify they pass**

Run: `python -m pytest tests/test_astrbot_latency_policy.py -q`

Expected: PASS。

### Task 2: 实现独立 Sub2API 管理 API 客户端和状态模型

**Files:**
- Create: `astrbot/sub2api_latency_guard/client.py`
- Create: `astrbot/sub2api_latency_guard/state.py`
- Test: `tests/test_astrbot_latency_client.py`

- [ ] **Step 1: Write tests for pagination and optimistic priority guard**

测试使用假的异步 HTTP 响应，验证 `/admin/usage` 按 `page_size=1000`、`created_at desc` 分页读取，能停止在 `last_usage_id`；更新 priority 前重新读取账号，当前值变化时拒绝覆盖。

- [ ] **Step 2: Implement `Sub2APIClient`**

客户端从 `base_url` 和运行时 admin API key/JWT 构造请求，提供：

```python
list_accounts()
list_usage(account_id=None, start_date=None, end_date=None)
get_account(account_id)
update_account_priority(account_id, expected_current, new_priority)
```

使用 `x-api-key` 优先、JWT 作为备用；超时 15 秒；响应非 2xx 抛出带状态码的异常；禁止记录凭据和完整响应。

- [ ] **Step 3: Implement atomic JSON state**

状态文件保存：最近处理的 usage ID、每个账号/模型/等级的滚动样本、人工 priority、上次插件写入值、惩罚状态、待确认提案和过期时间。使用临时文件加 `os.replace()`，插件退出前保存。

- [ ] **Step 4: Run client/state tests**

Run: `python -m pytest tests/test_astrbot_latency_client.py -q`

Expected: PASS。

### Task 3: 实现 AstrBot 插件轮询、同级账号分组和飞书通知

**Files:**
- Create: `astrbot/sub2api_latency_guard/main.py`
- Create: `astrbot/sub2api_latency_guard/config.schema.json`
- Create: `astrbot/sub2api_latency_guard/README.md`
- Test: `tests/test_astrbot_latency_guard.py`

- [ ] **Step 1: Write tests for same-tier proposals**

验证 Plus 账号变慢时只推荐健康 Plus；同级耗尽才推荐 Pro/上游；管理员手工修改 priority 后插件不覆盖；同一提案 nonce/TTL 过期后拒绝执行。

- [ ] **Step 2: Implement plugin lifecycle and configuration**

插件名 `sub2api_latency_guard`，默认每 60 秒轮询，默认 `mode=confirm`，支持 `observe|confirm|auto`；配置独立使用 `sub2api_base_url`、`sub2api_admin_api_key`、`sub2api_jwt`、通知目标、管理员 ID、采样窗口、`min_samples`、`pro_usage_threshold` 和最大 priority 惩罚。密钥只能来自 AstrBot 运行时配置。

- [ ] **Step 3: Implement polling and proposal generation**

轮询读取账号元数据和最新 usage，按 `account_id + model + plan/tier` 聚合 `first_token_ms`。规则先过滤失效账号，再计算同级基线和评价结果；延迟状态为 `slow/critical` 时从同级健康账号中生成替代建议；健康账号按 `manual priority + penalty` 排序。

- [ ] **Step 4: Implement Lark card and callback gates**

复用现有插件的 Lark platform lookup 和 card/message API，但使用独立状态 key。卡片包含样本、p50、p95、慢请求比例、基线、当前/建议 priority、同级替代账号和原因；确认回调校验管理员 ID、提案 nonce、10 分钟 TTL、当前 priority 未变化后才调用客户端更新。

- [ ] **Step 5: Implement optional AI explanation**

向 AstrBot 当前 provider 发送脱敏后的聚合 JSON，只要求返回中文解释和风险说明；插件忽略 AI 返回的账号 ID、priority 或执行命令字段，所有数值和写入动作来自确定性策略。

- [ ] **Step 6: Run plugin unit tests**

Run: `python -m pytest tests/test_astrbot_latency_guard.py -q`

Expected: PASS。

### Task 4: 文档、静态检查和交付验证

**Files:**
- Create: `docs/ASTRBOT_LATENCY_GUARD_CN.md`

- [ ] **Step 1: Document installation and safe modes**

文档说明插件目录、配置键、管理 API 权限、飞书卡片流程、`observe/confirm/auto` 语义、手工 priority 变更保护、回滚方式和不修改版本监控插件的边界。

- [ ] **Step 2: Run syntax and test checks**

Run: `python -m compileall astrbot/sub2api_latency_guard tests`

Expected: exit code 0。

Run: `python -m pytest tests/test_astrbot_latency_policy.py tests/test_astrbot_latency_client.py tests/test_astrbot_latency_guard.py -q`

Expected: all tests pass。

- [ ] **Step 3: Inspect diff and worktree**

Run: `git diff --check && git status --short`

Expected: only the new plugin, tests, plan and documentation are present; existing `.tmp/astrbot_sub2api_version_monitor.py` is untouched。

- [ ] **Step 4: Commit the implementation**

```bash
git add astrbot/sub2api_latency_guard tests docs/ASTRBOT_LATENCY_GUARD_CN.md docs/superpowers/plans/2026-08-07-astrbot-latency-guard.md
git commit -m "feat: 新增 AstrBot 账号延迟调度守卫"
```
