# 官方 v0.2.8 与定制功能的兼容约定

本项目保留官方 v0.2.8 的简易模式控制、OpenCode Go 用量管理和返利线下提现能力，同时保留定制的图片/视频存储、账号计费探测、Codex 门票和兑换码返利来源。正式部署仍遵循 [源码部署规范](SOURCE_DEPLOY_CN.md)，以定制仓库的 `origin/main` 为唯一来源。

## 账号受管状态

管理员保存账号时，仓库层在同一事务中通过 `FOR NO KEY UPDATE` 读取数据库当前值，再合并允许编辑的字段。计费探测快照、Ollama/OpenCode Go 用量状态和 Codex 门票不能直接被客户端旧值或伪造值覆盖。

- 计费探测开关独立控制是否允许探测；倍率同步依赖探测，不能反向启用探测。账号身份变化会使旧探测快照失效。
- Ollama 和 OpenCode Go 的用量开关、快照按平台、账号类型、API Key 和对应官方基址确定适用账号。OpenCode 专属平台的 Go/Zen 模式也参与判断。
- OpenCode Go 组身份不变时，从锁定的数据库行保留用量开关和快照；仅代理改变时保留开关、丢弃旧快照。普通更新、凭证更新和批量更新使用一致的身份判断。
- Codex 门票从数据库当前完整 `extra` 中保留；管理员传入的门票字段不能注入，也不能覆盖请求期间刚更新的门票。

## 返利记录与线下提现

返利记录按流水展示所有 `accrue` 入账，包含支付订单、兑换码以及无法关联来源的历史流水。保留 `ledger_id`、`source_type`、`source_id`、`redeem_code_id` 和 `redeem_code`，支持通过兑换码检索。

`order_id` 和 `invitee_id` 在关联记录不存在时返回 `null`。`order_amount` 与 `pay_amount` 优先取支付订单金额，兑换码来源取兑换码面值；两种来源均无法获取金额时返回 `null`，避免把未知金额表现为零。

管理员通过 `POST /api/v1/admin/affiliates/users/:user_id/withdraw` 登记已在站外完成的提现，请求体为 `{"amount": 金额}`，必须提供 `Idempotency-Key`。该接口只记录线下提现并扣减可提取返利额度，不代为打款、不增加站内余额。金额保留 8 位小数，未到期冻结额度不能提取。

同一个幂等键重复提交同一用户和金额时，返回首次流水结果并设置 `X-Idempotency-Replayed: true`，不重复扣减；更换用户或金额却重用该键会返回冲突。额度不足等失败会回滚占位流水，允许纠正后重试。提取记录同时包含 `transfer`（转入站内余额）和 `withdraw`（线下提现）。

界面金额沿用本项目的“灵石”单位。提现弹窗在输入、全部提取、可提取金额和成功提示中保留最多 8 位有效小数，与返利账本精度一致；普通返利列表继续复用统一的灵石格式化方法。

## 模型、调度与计费

模型目录包含 `gpt-6-sol`、`gpt-6-luna`、`claude-opus-5-5` 和 `grok-4.7`。GPT-6 Sol/Luna 的合法型号拼写使用各自的规范型号，`gpt-6` 仍解析为 `gpt-6-astra`。目录列出模型只表示网关具备对应识别和转发逻辑，实际调用能力取决于账号及上游支持。

管理员设置中的 `openai_oauth_scheduling_rate_multiplier` 支持三态更新：请求省略字段时保留现值，显式 `null` 清空全局覆盖，有限非负数字（包括 `0`）设置覆盖值。清空后 OAuth 账号调度使用账号自身的计费倍率；数据库从未保存过该设置时继续使用历史默认值 `1`。这项设置控制调度成本比较，不替代用户请求的计费定价。

Fast/priority 继续遵循定制报价路径：显式渠道价格和 Fast 倍率覆盖优先，默认目录的 Fast 兜底只补充缺失报价。视频任务仍按原任务标识去重，已经冻结费用的异步结算传递 `BalanceAlreadyHeld`，避免在结算路径再次扣减可用余额。

292/332 Codex 门票功能保持独立，按账号和模型隔离，保留总开关、模型不匹配失效策略和独立采集代理配置；普通账号编辑不能覆盖或注入数据库中已有的门票。

## 兼容图片接口

`gemini-` 前缀且包含图片型号标识的模型可以使用兼容的 `/v1/images/generations` 和 `/v1/images/edits` 路由，但只允许 API Key 账号承接。渠道映射产生 Gemini 图片模型后仍执行同样的 API Key 能力限制。

Gemini 图片 JSON 编辑请求保留原有引用 URL 和供应商扩展字段，仅按实际转发模型改写 `model`。原生 OpenAI 图片 JSON 编辑请求包含输入图片时仍转换为 multipart，复用原上传兼容流程。

## 客户端导入与用量导出

OpenAI 的 CC Switch 导入链接与内嵌 Codex 配置共用规范化后的 API 地址：去掉尾部斜杠，在未以 `/v1` 结尾时补充一次 `/v1`。导入继续使用 `gpt-5.6-sol`、`medium` 推理强度和 `goals = true`。

用户用量 CSV 以 UTF-8 BOM 开头，改善表格软件识别中文的兼容性。导出列保留请求时间、API Key 名称、模型、入口、用量、费用与耗时，不包含客户端 IP 和内部上游账号字段。

## 数据库迁移

`240_affiliate_ledger_operation_id.sql` 为 `user_affiliate_ledger` 增加可空的 `operation_id VARCHAR(64)` 和针对非空值的唯一索引，用于提现幂等。已有流水保持 `NULL`，无需回填，也不修改历史金额。

该文件与定制的 `240_openai_healthy_turn_state_shared_pool.sql` 保持原完整文件名共存。迁移器按完整文件名记录主键并逐文件校验 SHA-256，数字前缀相同不代表同一迁移。不得为调整数字顺序而重命名或改写已执行的迁移；已有数据库即使已执行后续序号，启动时仍会检查并应用遗漏的完整文件名。

迁移 241 的定向结构和值快照、失败回滚前恢复结构与认证缓存失效逻辑继续由 `release-prod` 和 `release-schema-compat` 负责。

## 简易模式和部署配置

`RUN_MODE=standard` 仍是默认值。简易模式提供两个独立选项：

- `SIMPLE_MODE_AUTO_CREATE_DEFAULT_GROUPS` 对应 YAML `simple_mode.auto_create_default_groups`，默认 `true`。环境变量留空时允许 YAML 生效；设为 `false` 只停止启动时补齐默认分组，不删除已有分组，也不改变运行时自动绑定。
- `SIMPLE_MODE_KEY_RATE_LIMIT_ENABLED` 对应 YAML `simple_mode_key_rate_limit_enabled`，默认 `false`。启用后只累计并检查 API Key 的 5 小时、每日和 7 日金额窗口，仍跳过余额与订阅扣费。数据库为窗口用量真值来源；限制在请求完成后累计，并发请求可能超出窗口额度，历史简易模式用量不回填。

四种 Compose 配置均传递上述选项，并保留定制的图片和视频本地存储参数；主 Compose 还保留异步图片任务的 S3 兼容对象存储配置。

发布仍使用受版本控制的 `release-staging`、`release-prod` 和门禁脚本。资源检查、完整提交和 staging run 绑定、镜像版本与能力检查、定价策略检查、容器及 HTTP 健康等待和失败回滚继续生效。staging 与 prod 使用隔离的配置、数据库、Redis、数据目录和端口；staging 验证通过不构成 prod 切换授权。
