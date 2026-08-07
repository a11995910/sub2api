# Sub2API 版本通知卡片生命周期 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task with verification checkpoints.

**Goal:** 让 AstrBot 版本通知在单群内只保留最新卡片，并通过带 SHA/nonce 的二次确认安全触发上游同步。

**Architecture:** 在 `sub2api_version_monitor` 状态文件中保存当前卡片和 pending 确认记录；Lark 适配器把卡片操作转换为普通命令。插件在发送新卡片成功后删除旧卡片，按钮处理经过管理员、上游 SHA、任务状态和 nonce/TTL 校验后才触发 GitHub Actions。

**Tech Stack:** Python 3.12、AstrBot plugin API、Lark OpenAPI Python SDK、aiohttp、GitHub Actions REST API、Docker Compose。

---

### Task 1: 明确远端基线并备份

**Files:**
- Remote read-only: `/opt/astrbot/data/plugins/sub2api_version_monitor/main.py`
- Remote read-only: `/opt/astrbot/compose.local.yml`
- Remote backup: `/opt/astrbot/data/plugins/sub2api_version_monitor/main.py.bak.lifecycle-<timestamp>`

- [ ] **Step 1: Record current plugin state and container health**

```bash
docker ps --filter name=astrbot
cat /opt/astrbot/data/plugin_data/sub2api_version_monitor/state.json
```

Expected: AstrBot running; no `runs.sync` or `runs.staging` created by the failed click.

- [ ] **Step 2: Back up the plugin before editing**

```bash
cp -p /opt/astrbot/data/plugins/sub2api_version_monitor/main.py \
  /opt/astrbot/data/plugins/sub2api_version_monitor/main.py.bak.lifecycle-$(date +%Y%m%d-%H%M%S)
```

### Task 2: Add message lifecycle helpers

**Files:**
- Modify remote `/opt/astrbot/data/plugins/sub2api_version_monitor/main.py`

- [ ] **Step 1: Add persisted state helpers**

Add helpers for `active_message_id`, `active_upstream_sha`, `active_fork_sha`, and `pending_update`, always saving through the existing `_save_state()` lock.

- [ ] **Step 2: Add Lark message deletion helper**

Use the existing platform `lark_api.im.v1.message.delete` request builder and delete only IDs persisted by this plugin. Treat HTTP/API not-found as an idempotent success; log other errors without failing a new notification.

- [ ] **Step 3: Store the sent card message ID**

Change the card send path to return the sent message ID. Persist the new ID and commit SHA only after the send succeeds, then delete the previous persisted card ID.

### Task 3: Implement two-step update confirmation

**Files:**
- Modify remote `/opt/astrbot/data/plugins/sub2api_version_monitor/main.py`

- [ ] **Step 1: Extend card payloads**

Keep the existing update/defer buttons bound to `upstream_sha`. Add confirmation and cancel buttons carrying `upstream_sha`, a cryptographically random nonce, and action names `confirm_update` / `cancel_update`.

- [ ] **Step 2: Gate the first update click**

On `button:update:<sha>`, re-check admin ID, current upstream SHA, `UPSTREAM_AHEAD`, and inactive sync/staging tasks. Save a pending record with a 10-minute UTC expiry and send a confirmation card; do not call GitHub.

- [ ] **Step 3: Gate confirmation and enforce idempotency**

On confirmation, require matching SHA, nonce, sender, non-expired pending record, and no active task. Clear pending state before dispatching `upstream-sync.yml`; if dispatch fails, restore a retryable pending state and report failure. A second confirmation after success must be rejected.

- [ ] **Step 4: Make defer terminal for the current card**

On `button:defer:<sha>`, persist `deferred_upstream_sha`, delete the active card, and send a result message. A newer upstream SHA clears the defer marker and creates the next card.

### Task 4: Verify without production changes

**Files:**
- Remote plugin log and state only

- [ ] **Step 1: Reload only AstrBot**

```bash
docker compose -f /opt/astrbot/compose.local.yml up -d --no-build --force-recreate astrbot
```

- [ ] **Step 2: Run focused callback tests**

Use a temporary harness based on `/tmp/card_test.py` to assert update/defer/confirm conversion and reject mismatched SHA/nonce. No GitHub dispatch is allowed in the harness.

- [ ] **Step 3: Verify card behavior manually**

Click `暂不更新` first and verify `deferred_upstream_sha` is persisted and no workflow run exists. Wait for or trigger a new notification, click `更新版本`, verify only a confirmation card appears and `runs` remains empty. Do not click confirmation until explicitly authorized for the staging test.

- [ ] **Step 4: Verify operational invariants**

```bash
docker ps --filter name=astrbot
docker logs --since 10m astrbot | grep -E "processor not found|sub2api_version_monitor|workflow"
```

Expected: AstrBot healthy, no callback processor errors, no Sub2API VPS or prod changes.

### Task 5: Document and commit the remote deployment state

**Files:**
- Modify `docs/SOURCE_DEPLOY_CN.md` if the final callback/mount path differs from the design.

- [ ] **Step 1: Record the actual AstrBot override path and reload command**
- [ ] **Step 2: Check `git diff --check` and commit only related docs**
- [ ] **Step 3: Push `main` before any future staging build**
