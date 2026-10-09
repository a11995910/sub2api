# 官方上游与定制功能的兼容约定

本项目基于官方 v0.2.15，保留简易模式控制、OpenCode Go 用量管理和返利线下提现能力，同时保留定制的图片/视频存储、账号计费探测、基于请求表现的智能轮候、Codex 门票和兑换码返利来源。正式部署仍遵循 [源码部署规范](SOURCE_DEPLOY_CN.md)，以定制仓库的 `origin/main` 为唯一来源。

## 平台清单与协议分流

后端具体平台统一登记于 `internal/domain/platforms.go`，前端内置清单与后端通过测试保持一致。Command Code 和 Cline 纳入账号、分组、配额与合成路由的平台选项；可同步模型、渠道监控和额度探测仍按各自已实现的能力开放。

多协议 API Key 供应商按平台 profile 提供各接入模式的默认端点，账号显式配置的端点和协议规则优先。Responses、Chat Completions 和 Messages 入口共用协议分流逻辑；定制的 Responses 转 Chat 回退仍保留推理条目 ID，以便从本地缓存恢复推理内容。OpenCode 会在转发前拒绝已知不支持的模型。

WebSocket 后续轮次通过认证缓存重新读取同一分组的定价快照，用于该轮利润门和计费；Key 换组、平台或订阅类型变化以及快照读取失败时沿用建连快照。定制的请求级定价时刻、活动折扣、图片输入费用及托管图片工具用量校验继续生效。

Antigravity 客户端错误先隐藏完整的项目标识和服务账号邮箱，再执行通用域名、IP 与查询参数脱敏，避免邮箱域名被替换后残留账号名前缀；错误体仅保留状态码、状态名和脱敏摘要。

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

模型目录包含 `gpt-6.1-sol`、`gpt-6-sol`、`gpt-6-luna`、`claude-opus-5-5`、`claude-sonnet-5-5` 和 `grok-4.7`。GPT-6 Sol/Luna 的合法型号拼写使用各自的规范型号，`gpt-6` 仍解析为 `gpt-6-astra`。目录列出模型只表示网关具备对应识别和转发逻辑，实际调用能力取决于账号及上游支持。

管理员设置中的 `openai_oauth_scheduling_rate_multiplier` 支持三态更新：请求省略字段时保留现值，显式 `null` 清空全局覆盖，有限非负数字（包括 `0`）设置覆盖值。清空后 OAuth 账号调度使用账号自身的计费倍率；数据库从未保存过该设置时继续使用历史默认值 `1`。这项设置控制调度成本比较，不替代用户请求的计费定价。

Fast/priority 继续遵循定制报价路径：显式渠道价格和 Fast 倍率覆盖优先，默认目录的 Fast 兜底只补充缺失报价。视频任务仍按原任务标识去重，已经冻结费用的异步结算传递 `BalanceAlreadyHeld`，避免在结算路径再次扣减可用余额。

GPT-6.1 Sol 纳入定制 Fast 能力表，缺失 priority 报价时按标准价的 2 倍补齐；未知产品后缀不会自动获得 Fast 能力。默认目录中 GPT-6 Sol/Luna 和 GPT-6.1 Sol 显式为零的缓存写入价格在 priority 档也保持免费。Astra 的 `ultrafast` 使用独立的 6 倍标准价，运营者配置的 Fast 倍率继续只控制 Fast/priority。GPT-6.1 Sol 兼容接口拒绝 `none`、`minimal` 推理等级和显式禁用思考，客户端目录保留官方能力元数据。

渠道图片输入/输出价格留空时继承模型目录价格；显式填写 `0` 表示该项免费，不能回退到文本价格。账号自定义统计规则仍保留原有空图片价格的文本价回退行为。账号统计的默认目录长上下文成本按账号开关计算，分组长上下文开关只控制客户售价；请求级计费时刻、服务档位和推理强度继续参与成本计算。

Anthropic 转 Chat Completions 时，只要上游携带 usage 就向客户端转发；缓存划拨启用时继续同步调整尾部 usage 与计费用量。合成分组的 Responses WebSocket 保留客户端模型做准入校验，按解析后的上游模型执行渠道映射，同一连接切换公开模型时拒绝并要求重新连接。

管理界面已移除网关服务中的 292/332 门票配置区，以及账号编辑中的状态头策略、缺票暂停和门票状态区。普通系统设置保存不再读取或提交门票配置字段；账号编辑保留既有门票策略及缺票暂停值，并继续将废弃健康头模式归一为关闭。后端历史门票配置和运行逻辑保留兼容，仍按既有配置执行；普通账号编辑不能覆盖或注入数据库中已有的门票。

## 并发余额预留与 API Key 创建限制

`billing.inflight_reservation.enabled` 默认启用，为余额模式的网关请求在 Redis 中登记估算费用。已有在途请求时，只有缓存余额扣除在途预留后足够覆盖新估算才放行；首个请求仍沿用原有余额准入，因此这不是严格的零透支保证。预留持续到请求结束且异步计费任务完成，计费时先同步扣减余额缓存再释放预留。简易模式和有效订阅请求不启用该机制，定制视频任务的数据库冻结与去重结算继续独立生效。

预留默认存活 900 秒，长请求期间续期；未指定输出上限时按 8192 token 估算，输入和输出估算上限分别为 200000、128000 token。Redis 故障默认放行；无法定价默认放行且不预留，可用 `fail_closed_on_unpriced` 改为拒绝。全部配置见 `deploy/config.example.yaml`。

`api_key_create.max_active_per_user` 默认限制每用户 200 个未删除的 API Key，`max_per_user_per_hour` 默认限制每用户每小时 60 次创建，各项设为 `0` 表示不限制。删除 Key 不返还创建次数，修改状态也不再清空创建失败计数；Redis 创建计数故障时沿用放行策略。

## Claude 手动额度重置

管理员可在账号额度单元格确认后调用 `POST /api/v1/admin/accounts/:id/claude/reset-credits/redeem`，请求必须携带 `Idempotency-Key`。服务端重新查询可用资格并选择重置凭据，通过幂等记录、账号及组织锁避免重复兑换；结果未确认时保留组织级保护，避免重试再次消耗额度。客户端仅显示经过筛选的结果和额度窗口，不接收上游凭据或组织标识。

## 兼容图片接口

`gemini-` 前缀且包含图片型号标识的模型可以使用兼容的 `/v1/images/generations` 和 `/v1/images/edits` 路由，但只允许 API Key 账号承接。渠道映射产生 Gemini 图片模型后仍执行同样的 API Key 能力限制。

Gemini 图片 JSON 编辑请求保留原有引用 URL 和供应商扩展字段，仅按实际转发模型改写 `model`。原生 OpenAI 图片 JSON 编辑请求包含输入图片时仍转换为 multipart，复用原上传兼容流程。

## 客户端导入与用量导出

OpenAI 的 CC Switch 导入链接保留所配置的端点，仅去掉尾部斜杠，避免客户端重复追加 `/v1`；内嵌 Codex 配置的 `base_url` 保持单个 `/v1` 后缀。用量查询脚本先移除已有 `/v1`，再拼接 `/v1/usage`，金额缺省单位为“灵石”。导入继续使用 `gpt-5.6-sol`、`medium` 推理强度和 `goals = true`。

使用 Key 弹窗生成的 Codex 配置默认采用 `model_catalog_url`，指向站点的 `/v1/models`；也可切换为本地 JSON 文件模式。远程目录无需手动下载模型文件；默认模型、推理等级和 `goals` 定制设置保留。账号订阅标识支持上游当前的 Pro、Team、Business 等计划名称。

用户用量 CSV 以 UTF-8 BOM 开头，改善表格软件识别中文的兼容性。导出列保留请求时间、API Key 名称、模型、入口、用量、费用与耗时，不包含客户端 IP 和内部上游账号字段。

## 数据库迁移

`242_drop_platform_check_constraints.sql` 仅移除 `user_platform_quotas.platform` 和 `composite_model_routes.target_platform` 的固定白名单约束，校验改由接口、服务、仓库与 Ent 的共享平台清单承担；不删除业务数据，不修改渠道监控的能力约束。它与定制的 242 迁移按完整文件名独立执行。回滚应用可保留放宽后的约束，但旧版本不识别 Command Code、Cline 等新增平台，回滚前须检查并停用相关账号和路由，避免旧版处理未知平台数据。

`240_affiliate_ledger_operation_id.sql` 为 `user_affiliate_ledger` 增加可空的 `operation_id VARCHAR(64)` 和针对非空值的唯一索引，用于提现幂等。已有流水保持 `NULL`，无需回填，也不修改历史金额。

该文件与定制的 `240_openai_healthy_turn_state_shared_pool.sql` 保持原完整文件名共存。迁移器按完整文件名记录主键并逐文件校验 SHA-256，数字前缀相同不代表同一迁移。不得为调整数字顺序而重命名或改写已执行的迁移；已有数据库即使已执行后续序号，启动时仍会检查并应用遗漏的完整文件名。

迁移 241 的定向结构和值快照、失败回滚前恢复结构与认证缓存失效逻辑继续由 `release-prod` 和 `release-schema-compat` 负责。

## 简易模式和部署配置

`RUN_MODE=standard` 仍是默认值。简易模式提供两个独立选项：

- `SIMPLE_MODE_AUTO_CREATE_DEFAULT_GROUPS` 对应 YAML `simple_mode.auto_create_default_groups`，默认 `true`。环境变量留空时允许 YAML 生效；设为 `false` 只停止启动时补齐默认分组，不删除已有分组，也不改变运行时自动绑定。
- `SIMPLE_MODE_KEY_RATE_LIMIT_ENABLED` 对应 YAML `simple_mode_key_rate_limit_enabled`，默认 `false`。启用后只累计并检查 API Key 的 5 小时、每日和 7 日金额窗口，仍跳过余额与订阅扣费。数据库为窗口用量真值来源；限制在请求完成后累计，并发请求可能超出窗口额度，历史简易模式用量不回填。

四种 Compose 配置均传递上述选项，并保留定制的图片和视频本地存储参数；主 Compose 还保留异步图片任务的 S3 兼容对象存储配置。

发布仍使用受版本控制的 `release-staging`、`release-prod` 和门禁脚本。资源检查、完整提交和 staging run 绑定、镜像版本与能力检查、定价策略检查、容器及 HTTP 健康等待和失败回滚继续生效。staging 与 prod 使用隔离的配置、数据库、Redis、数据目录和端口；staging 验证通过不构成 prod 切换授权。
