# Sub2API 版本通知卡片生命周期设计

## 目标

版本监控只管理一个飞书通知群。群内始终只保留最新的一张版本通知卡片，避免旧卡片误触发同步。所有更新操作都必须绑定上游 commit，并经过二次确认；任何生产发布仍由独立的 GitHub production environment 门禁控制。

## 状态与消息

插件状态文件增加当前通知记录：

- `active_message_id`：当前版本卡片的飞书消息 ID。
- `active_upstream_sha`：卡片绑定的上游 commit SHA。
- `active_fork_sha`：发送卡片时 fork 的 `main` commit SHA，仅用于审计和提示。
- `pending_update`：二次确认状态，包含上游 SHA、发起人、确认消息 ID 和过期时间。
- `deferred_upstream_sha`：用户选择暂不更新时记录的上游 SHA。

发送新版本卡片时，先发送新卡片，成功取得消息 ID 后再删除旧卡片。删除失败只记录日志，不阻止新卡片发送。插件重启后保留上述状态，不能因为进程重启重复发送或误删其他消息。

## 按钮流程

### 更新版本

第一次点击只做校验，不触发 GitHub Actions：

1. 校验管理员 sender ID。
2. 校验按钮中的上游 SHA 等于当前轮询快照中的上游 SHA。
3. 校验当前状态仍为 `UPSTREAM_AHEAD`。
4. 校验没有正在运行的同步或 staging 任务。
5. 写入一个 10 分钟有效的 `pending_update`，发送二次确认卡片。

二次确认按钮必须携带同一上游 SHA 和确认 nonce。确认时再次执行上述校验；校验成功后才触发 `upstream-sync.yml`，并立即清理当前版本卡片和确认卡片。取消或超时只清理确认状态，不触发任何 workflow。

### 暂不更新

点击后校验管理员和上游 SHA，写入 `deferred_upstream_sha`，删除当前版本卡片，并发送一条结果消息。只要上游 SHA 不变，轮询不重复通知；当上游出现更高 commit 时，清除延期状态并发送新的卡片。

### 过期卡片

卡片 SHA 与当前快照不一致时直接拒绝，不触发 workflow，并提示用户等待最新通知。不能通过二次确认绕过过期校验。

## 并发与安全

- 使用异步锁保护状态读写和按钮决策，确保重复点击最多创建一个 pending 状态或一个 workflow。
- 上游同步、staging、prod 仍由现有 workflow 和状态校验串联；卡片逻辑不直接操作 VPS 或生产容器。
- 删除消息只允许使用插件记录的 `active_message_id` 或确认消息 ID，不按群历史批量删除，避免误删其他消息。
- GitHub token、飞书密钥和消息内容中的敏感信息不得写入日志或仓库。

## 验证场景

1. 新版本通知只留下最新卡片，旧卡片被删除或删除失败时不影响新卡片。
2. 第一次点击更新只收到二次确认，不产生 GitHub workflow run。
3. 确认更新只产生一个 `upstream-sync` run；重复点击不会产生第二个 run。
4. 暂不更新后同一上游不再提醒，新上游 commit 会重新提醒。
5. 旧卡片、旧确认卡片和错误 sender ID 均不能触发同步。
6. AstrBot 重启后 pending 状态按过期时间处理，active 消息记录仍可清理。
