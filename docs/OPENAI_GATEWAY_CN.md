# OpenAI 网关功能说明

## 功能入口

OpenAI 平台分组的 API Key 可通过 OpenAI 兼容入口调用文本、图片和向量能力。网关统一走现有 API Key 鉴权、分组校验、账号调度、并发控制、余额与配额校验、用量记录和计费链路。

| 入口 | 说明 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions 兼容入口。OpenAI 分组走 OpenAI 网关，非 OpenAI 分组按既有 Claude/Gemini 兼容链路处理。 |
| `POST /v1/responses` | OpenAI Responses 兼容入口，支持子路径 `/v1/responses/*subpath`。 |
| `GET /v1/responses` | OpenAI Responses WebSocket 入口。 |
| `POST /v1/embeddings` | OpenAI Embeddings 兼容入口，仅 OpenAI 分组可用。 |
| `POST /v1/images/generations` | OpenAI 图片生成入口，仅 OpenAI 分组可用。 |
| `POST /v1/images/edits` | OpenAI 图片编辑入口，仅 OpenAI 分组可用。 |
| `POST /chat/completions`、`POST /responses`、`POST /embeddings`、`POST /images/generations`、`POST /images/edits` | 不带 `/v1` 前缀的兼容别名，鉴权、调度和错误处理与 `/v1` 入口一致。 |
| `POST /backend-api/codex/responses` | Codex 直连兼容入口，内部复用 Responses 网关处理。 |

非 OpenAI 分组访问 Embeddings 或图片入口时，网关返回 `404`，错误类型为 `not_found_error`，并记录本地功能门禁类运维限制标记。

## Responses 转 Chat Completions 兼容

当 OpenAI API Key 账号被配置为强制使用 Chat Completions，或上游能力探测确认不支持 Responses 时，`POST /v1/responses` 会在网关内转换为 Chat Completions 请求。custom、`tool_search`、namespace/MCP 工具和工具结果会按 Chat Completions 结构转发，响应再恢复为 Responses 事件或非流式响应。

转换链会按 Responses reasoning item ID 缓存完整 `reasoning_content`，缓存有效期为 7 天。后续历史仅携带 `encrypted_content` 而没有明文 summary 时，网关按该 ID 回查并补回 assistant 工具调用消息；缓存读取失败或未命中时保持兼容降级，不中断请求。每个显式 reasoning item 都开启独立思考片段，不会错误继承前一个工具调用的明文；请求中重新出现明文 summary 时会刷新缓存。

Raw Chat 回退在内部转换完成前保留 reasoning item ID，使缓存回查可用，但该 ID 不会进入最终 Chat Completions 上游请求。原生 OpenAI Responses、透传和 WebSocket 出站仍会删除不符合 OpenAI 前缀约束的回放 item ID，避免上游因无效 ID 返回 `400`。

## 上游错误可见性

OpenAI 网关及 Codex 直连入口不会向客户端回传基础设施类上游错误体。上游返回 `5xx`（包括 Cloudflare 的 `520` 至 `524`）时，即使命中管理端错误透传规则，客户端只会收到本地通用文案 `Upstream service temporarily unavailable`；上游域名、CDN 区域、回源地址、IP、错误详情和原始 JSON 不会出现在 HTTP 或 SSE 错误响应中。

参数校验等 `4xx` 错误可按错误透传规则保留可操作提示。网关会从该提示中隐藏 HTTP/HTTPS URL、域名、IP 和 `key`、`client_secret`、`access_token`、`refresh_token` 查询参数。上游错误体仅用于账号状态判断、故障切换和受控运维日志；是否记录错误体摘要由 `gateway.log_upstream_error_body` 配置控制，不影响客户端响应。

该规则覆盖 `/v1/responses`、`/responses`、`/backend-api/codex/responses`、Chat Completions、Embeddings 及其流式错误终止事件。流式响应开始后，Codex Responses 入口仍以 `response.failed` 事件返回本地安全文案，避免连接直接结束。

## API Key 层 OpenAI Fast 模式

用户侧 API Key 创建和编辑接口支持布尔字段 `openai_fast_mode_enabled`，数据库列为 `api_keys.openai_fast_mode_enabled`，默认值为 `false`。开启后，该 Key 发起的 OpenAI 网关请求如果没有显式携带 `service_tier`，服务端会在转发上游前默认补入 `service_tier="priority"`。

该开关只补默认值，不覆盖客户端显式选择：客户端已经传入 `priority`、`flex`、`auto`、`default` 或 `scale` 时，网关尊重原值并继续按现有归一化逻辑处理。补入后的 `priority` 仍会经过全局 OpenAI fast policy，因此管理员配置的 `filter` 或 `block` 规则仍然生效。最终保留下来的 `service_tier` 会进入用量日志和计费链路，用于区分 priority/flex/default 等成本。

覆盖入口包括 `/v1/chat/completions`、`/v1/responses`、Anthropic Messages 到 OpenAI Responses 的兼容入口，以及 OpenAI Responses WebSocket/Realtime 路径。非 OpenAI 网关链路不会读取该开关。

系统级 Fast/Flex 策略还可以通过 `user_ids` 限定 Sub2API 用户。用户 ID 来自鉴权后的 API Key 所属用户，不读取客户端请求体；用户专属规则优先于全局规则，组内仍按配置顺序首条命中。管理端只接受大于 0、不重复的安全整数。

该配置保存在 `openai_fast_policy_settings` JSON 中。回滚到不支持 `user_ids` 的旧版本前，必须恢复发布前设置快照或删除所有带 `user_ids` 的规则，否则旧版本会把用户专属规则按全局规则执行。

## 账号调度与粘性会话

OpenAI 账号调度使用账号 `priority`、运行时并发负载、最近使用时间、模型能力、分组归属和运行态共同决定候选账号。`priority` 数值越小优先级越高；账号 `status` 不是 `active`、`schedulable=false`、临时不可调度、运行时被限流或不支持当前入口能力时，不会进入本次候选。

请求携带 `session_id`、`conversation_id`、`prompt_cache_key`，或可从请求内容生成稳定会话种子时，网关会维护 OpenAI 粘性会话绑定。粘性会话用于让同一会话尽量继续使用同一上游账号，减少上下文漂移；绑定账号不可用、离开当前分组、模型或入口能力不匹配、运行态被阻断时，系统会清理绑定并重新按候选池选择。

负载感知调度启用时，如果粘性会话绑定的账号优先级低于当前分组内另一个可用 OpenAI 账号，并且该更高优先级账号的运行时负载未满，系统会清理旧粘性绑定并回到优先级排序重新选择。更高优先级账号已满载、不可用、被排除、不支持当前请求或与粘性账号同优先级时，系统会保留粘性绑定；若绑定账号自身没有可用并发槽，则按粘性会话等待配置生成等待计划。

## 请求完整性观察

OpenAI 账号默认使用 `extra.request_integrity_mode="observe"`；账号编辑页可以改为 `off`。创建、编辑、批量更新与 Extra 更新接口仅接受 `observe`、`off`，非法值返回 `400 INVALID_REQUEST_INTEGRITY_MODE`。该设置独立于指纹收敛，不修改请求、不拦截、不触发账号冷却或评分，也不自动提高推理档位。

观察范围是进入 Responses 转发阶段的请求与上游发送载荷，包括 HTTP、HTTP 透传、HTTP 转 WS、原生 WS 透传、WS 连接池及 WS 转 HTTP bridge。WS 每轮请求保留独立快照，重放和兼容重试仍与该轮入站快照比较。Chat Completions / Messages 转换前的原始协议、Responses 转 Raw Chat 的最终载荷、图片与向量专用入口不在此检查范围内，不能把没有观察日志理解为整条协议转换链已验证无损。

观察字段包括模型、`input`、`instructions`、`reasoning`、工具定义与选择、工具并行参数、文本格式、历史响应关联、推理回放返回项、上下文管理、截断策略、工具调用预算和 API Key 端点的输出预算。只检查入站存在字段的变化或丢失；补入默认字段不报告。字符串输入、纯文本系统指令提升、函数嵌套等已知等价表示先归一化；模型映射按账号实际配置处理。设备、会话等身份元数据不参与摘要；Codex 订阅端点不支持的 `max_output_tokens` / `max_completion_tokens` 不参与该端点比较。

快照仅在当前请求内保存字段摘要。变化日志名为 `openai_request_integrity_observation`，只包含账号 ID、模式、转发路径、状态和固定字段名，不记录对话、工具参数、凭据或摘要值。`changed` 表示需要结合策略检查的差异，不能直接判定模型降智；管理员工具策略、上下文重放及兼容降级也可能产生差异。超过 4 MiB 或无法解码的请求分别记录 `skipped_size_limit` / `skipped_invalid_json`，不视作检查通过。无变化时不写观察日志；同一快照连续相同的重试报告去重。

## HTTP 与 WS 身份一致性

原生 WS 首帧、后续帧与连接池入口沿用 HTTP 的身份处理顺序：先按凭据隔离，再应用账号现有的 `codex_fingerprint_mode`。缺省仍为 `off`；`device`、`session`、`full` 的种子和派生规则保持一致。同轮握手头与请求体共享身份，后续轮次刷新 turn ID；已存在的元数据别名同步更新。启用收敛后连接池会检查会话和 conversation，避免复用其他会话的旧握手。

身份修复自动生效，实际收敛强度仍由原指纹开关控制；请求完整性观察可以与任意档位同时使用。TLS 指纹由现有配置独立控制。

## 重试与状态头处理

### 状态头模式与对比

OpenAI OAuth／SetupToken 账号编辑页使用 `extra.openai_turn_state_mode`，仅支持以下两种选择：

| 模式 | 采集与业务行为 | 缺票策略 |
| --- | --- | --- |
| `off` | 关闭额外采集与注入，保留客户端原有会话状态头处理 | 原有转发 |
| `codex_ticket` | 采集 292/332 门票，按账号与实际模型保存门票及成功代理，后续请求复用同一绑定 | 默认暂停缺票账号对应模型，可用 `extra.openai_codex_ticket_fail_closed=false` 放行 |

新建独立 OpenAI OAuth／SetupToken 账号未指定模式时默认 `off`，与订阅档位无关。存量 `healthy_retry`、`healthy_preflight`、未知模式及旧 `openai_healthy_turn_state_replace` 开关均按关闭处理，不自动切换到门票模式。管理接口新写入模式只接受 `off`、`codex_ticket`，其他值返回 `400 INVALID_TURN_STATE_SETTING`；创建和更新校验会移除传入的旧 `openai_healthy_turn_state_record`、`openai_healthy_turn_state_replace`、`openai_healthy_turn_state_fail_closed` 字段。

健康头库存、动态配置、模型查询及采集运行接口 `/api/v1/admin/accounts/:id/healthy-turn-state` 与其子路径已下线；服务不再启动旧维护任务或读写旧库存。历史迁移 239、240、242、243 及其数据表、加密设置保留，不执行破坏性清库；旧设置不参与门票采集，门票来源仍由独立系统设置管理。

缺票且换号耗尽时返回 HTTP 503，错误码为 `turn_state_unavailable`。Messages 兼容入口保留 `api_error` 格式。已开始发送的流无法更改 HTTP 状态码，改为发送协议错误并在运维记录目标状态 503；本地缺票不计作代理故障或账号健康度失败。

### 292/332 门票模式

该模式需要同时打开系统设置中的“292/332 门票总开关”并在账号选择 `codex_ticket`。配置独立采集来源后，后台对启用该模式的活跃、非影子 OpenAI OAuth 类账号采集。来源支持固定代理地址和批量 IP 提取接口。成功采集后，门票和本次代理完整地址按账号、实际模型共同保存，业务请求默认使用这条代理，可通过下述独立开关恢复账号原出口。代理入口不等于实际出口 IP：供应商必须提供固定或粘性出口，且粘性时长应覆盖 `ttl_seconds`；轮换入口即使地址不变，也不能保证请求来自同一个 IP。专用采集连接使用 HTTP/1.1 并禁用连接复用。

后台设置字段为 `openai_codex_ticket_enabled`、`openai_codex_ticket_harvest_proxy_mode`（`proxy`／`extract`，默认 `proxy`）、`openai_codex_ticket_harvest_proxy_url`、`openai_codex_ticket_harvest_extract_url` 和 `openai_codex_ticket_harvest_extract_protocol`（`http`／`https`／`socks5h`，默认 `http`）。固定代理和提取接口独立保存，切换来源不会清空另一来源；代理凭据和提取 URL 路径、查询参数在读回及审计中脱敏，空值或脱敏占位保留已保存值。YAML／环境变量使用 `gateway.openai_codex_ticket`：`target_length=292`、`ttl_seconds=3600`、`refresh_before_seconds=600`（仅保留配置兼容，不再提前换票换出口）、`harvest_probe_interval_seconds=6`（账号发现间隔及单目标失败重试间隔）、`harvest_attempt_timeout_seconds=25`、`harvest_proxy_concurrency=3`（范围 1–8）、`harvest_global_concurrency=8`（范围 1–32）、`fail_closed=true`；默认模型为 `gpt-6-astra`、`gpt-5.6-sol`，其他模型可通过 `models` 配置。总开关默认关闭，运行时设置约五秒缓存。

总开关下方提供两个独立行为开关，保存后热更新，无需重启；旧配置未保存这些字段时均默认开启，其他设置的部分更新不会重置它们：

| 开关／API 字段 | 开启 | 关闭 |
| --- | --- | --- |
| 模型不一致（含路由到 Luna）弃票／`openai_codex_ticket_model_mismatch_invalidation` | 采票发现模型偏移不入库，业务响应发现模型偏移淘汰本代票 | 允许上游声明模型偏移；仍要求采票有模型声明、成功终态与合法状态头，并保留业务网络失败及明确失效错误弃票 |
| 后续请求使用打票代理 IP／`openai_codex_ticket_use_harvest_proxy` | HTTP／WS 业务请求复用成功采票代理 | 仍注入门票，但使用账号原代理；未配置账号代理时直连。后台采票仍使用独立采集来源 |

这两个开关不会恢复已经失效的门票。代理开关变化后，新 HTTP 请求使用新策略；池化 WS 的下一独立轮次重新握手，续链或原始 WS 透传发现出口改变时要求重新连接。正在处理的响应继续使用已有连接。这里没有专用的“15 秒首字超时弃票”设置：采票采用上述总超时，业务首字等待由通用网关超时策略控制。

批量提取接口支持公网 HTTP／HTTPS URL，返回按行分隔的 `IP:端口`、代理 URL 文本或下述 BestGo JSON；无协议的条目使用所选代理协议。接口原查询参数保持不变，例如 `num=10` 表示供应商返回十个代理，系统不会改写 `num`、`time` 等参数。每个缺票的账号／模型在独立采集任务中提取一次，提取结果仅供该目标本次并发探测使用；提取请求禁止重定向，超时 20 秒，响应上限 64KiB。全进程最多四个账号／模型同时提取或采集，每目标默认并发探测三个不同代理，全局默认最多八个探测请求；固定代理每目标仅一路。第一个通过完整校验的门票获胜，取消并关闭其余探测，仅保存获胜票及对应代理。失败候选释放并发位后继续尝试本批其他代理；整批失败后，该目标从自身结束时起等待约六秒再提取，不等待其他目标结束。并发取消不能保证上游尚未产生计费或额度消耗。

有效门票继续复用，目标任务按票的到期时间独立唤醒；弃票及请求侧缺票检查立即唤醒对应任务。账号列表定期发现新目标；同一账号模型的多个请求合并到一组任务，IP 提取也包含在去重范围内。重复缺票检查不会突破失败重试间隔，所有触发仍受全局并发上限约束。停用账号、关闭总开关或切换模式后停止相应采集；采集前及保存前复核账号资格和来源，旧来源结果不会保存。进程停止取消提取、探测、排队和独立计时，等待任务退出。

业务请求仍优先选择已有有效票的账号；不在请求链路等待新票，不并发重放业务正文，无可用账号时保留现有错误语义。后台并发使用简短探测请求，拿到响应头尚不算成功，仍须完成模型声明和成功终态校验（模型一致检查受对应开关控制）。每目标独立提取会增加供应商提取次数；代理有效期由供应商决定，接口参数 `time` 不改变门票本地期限。

门票要求 HTTP 200、状态头长度符合配置规则、前缀为 `gAAAAA`，并且有探测响应模型声明、响应成功完成；开启模型不一致弃票时还要求声明与实际出站模型一致。`target_length` 默认 292，配置为 292 或 332 时同时接受这两种长度，其他自定义值仍精确匹配；默认不接受 356。采集入库、重启恢复、调度、状态展示及 HTTP／WS 请求复用使用同一规则，保存的 `length` 必须等于实际头长度；采集读取 JSON 或 SSE 到成功终态，开启模型不一致弃票时的模型偏移，以及缺少模型声明、流内错误或提前断流均不入库，并继续尝试下一代理。探测仍受原超时约束，单个事件／JSON 正文最多 1 MiB；收到成功终态后取消读取并关闭流，不等待连接结束。探测会消耗一次简短模型响应；模型一致只能验证上游声明，不能保证回答质量。每账号、实际出站模型保存一组门票和成功代理，允许并发复用；默认一小时为本地期限，有效期间不提前换票或换出口。门票与代理存入账号服务端 `extra`，账号编辑不能自行写入，管理 DTO、审计与导出均移除门票及代理凭据。旧门票缺少 `proxy_url` 时视为不可用，由后台重新采集；无需数据库迁移。重启会恢复已持久化的完整绑定。

开启模型不一致弃票时，业务响应的协议字段声明模型与门票绑定的实际出站模型不一致（例如请求 `gpt-6-astra`，返回 `gpt-5.6-luna`），或业务出站连接错误、超时、HTTP 403／407／502／504，或明确的状态头失效错误码（`invalid_turn_state`、`turn_state_invalid`、`turn_state_expired`、`invalid_codex_turn_state`、`codex_turn_state_expired`）会将该绑定整体标为失效，立即唤醒对应目标重新采集；403 等状态按不可继续使用该绑定处理，不代表已确认其物理 IP 离线。客户端取消、401、429、普通 500／503 不直接淘汰绑定，继续原有鉴权、限流和错误处理。失败不把旧门票拿去其他代理补试；缺票期间遵循原 `fail_closed` 设置，默认暂停对应账号模型。失效标记持久化并保留版本信息，旧账号快照与旧请求的迟到失败不会恢复旧票或淘汰新绑定。模型判定与用量记录的模型不一致标记使用同一比较规则，只读取协议模型字段，不扫描输出文字或工具参数。HTTP JSON／SSE、池化 WS 和原始帧透传均观察模型声明；开启对应开关时，发现不一致立即标记本代绑定失效，管理端显示原因，后续请求不再复用。正在转发的响应保持原字节，不自动回放；已有输出无法撤回。供应商静默更换出口无法仅靠业务状态码可靠识别。

B2Proxy 的 [API 提取说明](https://help.b2proxy.com/zh/proxy-settings/residential/api-proxy-extraction) 将会话分为轮换 IP 和粘性 IP：轮换模式每次请求更换出口；粘性模式默认五分钟，需将“提前更换 IP”设为“否”，并使门票期限不超过实际粘性时长。应在供应商面板生成相应提取链接，不猜测或自动改写其查询参数。

HTTP Responses、透传、Messages 兼容桥及 WS 握手从同一门票快照选取请求头和代理，避免并发刷新造成错配；compact 按实际出站模型判定，非配置模型不缺票拦截。获取 WS 连接时按账号模式、票摘要和代理摘要匹配，独立建连或重连按本轮实际模型读取完整绑定；池化会话的独立轮次更换模型或绑定时重新握手。带 `previous_response_id` 的续链只允许原绑定继续使用，绑定到期或改变时要求重新开始会话；原始 WS 帧透传在更换模型或绑定后要求客户端重新连接，避免在旧连接上发送另一模型的票。

账号响应 `codex_turn_tickets` 提供各模型的 `ready`、`remaining_seconds`、`blocked`、到期与到期重采时间、`invalid_reason`（模型不一致或上游绑定失效），并包含 `harvest_status`、`last_attempt_at`、`last_result`、`last_http_status`、`attempt_index`／`attempt_total` 和 `next_attempt_at` 等采集摘要，不返回票原文、代理地址或原始错误。编辑页和账号列表每五秒刷新状态；编辑中的未保存字段不受刷新影响。`attempt_index` 表示本次已发起的代理探测数，`attempt_total` 为本批候选数量，不代表串行位置。失败后显示该账号模型独立安排的下一次重试时间；全局并发排队可能延后实际请求，没有确定时间时显示等待采集并发位。最近采集结果仅保存在进程内，重启后清空；已持久化且包含成功代理的有效绑定继续可用。对比时使用相同模型、相近账号条件和相同时间窗口的独立账号／分组，对照请求成功率、429／503、模型不一致、首字延迟及采集消耗。门票的就绪状态也不等于业务成功率。

292/332 门票的批量提取使用独立安全解析器，兼容 BestGo 的 JSON 返回：`{"code":200,"success":"success","msg":"","error":"","data":[{"ip":"8.8.8.8","port":8080}]}`。`ip` 必须是公网字面 IP，`port` 必须是 1–65535 的整数；任一条目无效时拒绝整批。成功空列表按无新入口处理。供应商返回 `IP Whitelist Check Failed` 时显示固定白名单提示，其他错误不透传供应商正文。接口地址中的 `count=1` 仅控制供应商单批数量，`stype=json` 指定 JSON 返回，`sessType=rotating` 由供应商解释；所有查询参数原样传递，不改变采集上限、间隔或代理入口去重语义。地址回显保留实际 HTTP／HTTPS 协议和端口，路径与参数始终脱敏。

### 常规退避

OpenAI OAuth / SetupToken 的瞬时 `429` 使用原有的 2 分钟同账号重试窗口，不额外设置固定 3 次上限。普通重试默认间隔 500 毫秒；上游 `Retry-After` 指定等待时，最多等待 8 秒，并按剩余窗口截断。窗口结束后进入账号切换或错误处理。其他错误继续使用各自的次数限制和退避规则。

通用账号限流按当前响应的恢复时间写入，后到的短冷却可以覆盖此前较长的冷却；平台专用的条件更新仍遵守各自规则。显式管理恢复与成功账号测试可清理可恢复状态。

## 图片上游兼容模式

### 图片响应格式与临时 URL

图片分组可在管理端设置默认传输方式 `image_response_format`，取值为 `b64_json` 或 `url`，默认 `b64_json`。该配置只在客户未传 `response_format` 时生效；客户在 `/v1/images/generations` 或 `/v1/images/edits` 请求中显式传入的格式优先。

URL 模式会在超分或 2K/4K 二段增强完成后，将最终图片保存到 `IMAGE_STORAGE_PATH`，并返回以下形式的公开地址：

```text
https://<API 域名>/generated-images/<随机文件名>.<扩展名>
```

公开地址无需 API Key，文件名使用不可枚举随机值。图片从创建起保存 24 小时；满 24 小时后读取端点立即返回 404，后台任务负责定期删除过期文件。服务优先使用系统设置中的“API 端点地址”生成绝对地址；未配置时返回同域相对路径，不读取客户端可控的 Host 或转发头。

URL 模式统一处理上游 Base64、Data URL 和 HTTP/HTTPS 图片 URL，并按真实文件内容识别 PNG、JPEG 或 WebP；单图最大 64MB。存储失败会返回系统错误，不会自动改成 Base64。流式请求的 `partial_image` 事件继续返回 Base64 且不落盘，只有最终 `completed` 图片保存并返回 URL。

单实例可直接使用本地持久卷。多实例部署必须让所有实例共享 `IMAGE_STORAGE_PATH`，否则生成请求和图片读取落到不同实例时会返回 404。

OpenAI 图片入口默认按原生 Images API 转发：`/v1/images/generations` 转到上游 `/v1/images/generations`，`/v1/images/edits` 转到上游 `/v1/images/edits`。若某个 OpenAI APIKey 上游只支持 `/v1/chat/completions` 生成图片，可在该分组绑定渠道的 `features_config` 中配置：

```json
{
  "openai_images_upstream": {
    "mode": "chat_completions"
  }
}
```

启用后，该渠道下的 `/v1/images/generations` 和 `/v1/images/edits` 请求仍对下游保持 OpenAI Images API 形态，但网关会把请求转换为非流式 Chat Completions 请求发送到上游 `{base_url}/v1/chat/completions`。文生图请求发送普通文本消息；图生图请求会把本地 multipart 上传图片转成 `data:image/*;base64`，并与 JSON 请求中的 `images[].image_url` 一起放入 `messages[].content[]` 的 `image_url.url` 多模态字段。上游返回的 Markdown 图片、普通图片 URL、`data:image/*;base64` 或 JSON 字段 `url`、`image_url`、`b64_json` 会被重新包装为 Images API 响应，并继续进入图片计费和用量记录链路。

该模式只支持 OpenAI APIKey 账号和非流式图片入口，不支持图片流式返回。未启用该配置时，图片入口仍要求 `gpt-image-*` 这类原生 OpenAI 图片模型，避免普通文本模型被误识别为图片模型。

## 图片 4K 提升分组

OpenAI 图片分组支持在管理端开启 `4K 提升`，并选择另一个允许生图的 OpenAI 图片分组和目标模型作为提升目标。典型用法是：当前分组先使用 image2 生成基础图片；当请求命中 4K 档位时，再把第一段图片交给 `nano-banana-2` 这类图片模型做二段提升，最终把提升后的图片按原 OpenAI Images API 响应返回给下游。

触发条件如下：

1. 当前 API Key 绑定的分组为 OpenAI 平台，且 `allow_image_generation=true`。
2. 当前分组开启 `image_4k_enhancement_enabled=true`，并配置有效的 `image_4k_enhancement_group_id` 和 `image_4k_enhancement_model`。
3. 请求为非流式图片生成或图片编辑，且 `size` 解析后的计费档位为 `4K`。
4. 目标分组必须是另一个启用状态、允许图片生成的 OpenAI 分组；管理端保存时会拦截缺少目标分组、目标模型、目标分组指向自己、目标分组不存在或目标分组不允许生图的配置。

二段提升会走内部 `/v1/images/edits` 请求，把第一段结果作为参考图传给目标分组。请求中的原始 `size` 会原样传递到二段提升：提示词会明确包含原始尺寸，例如 `3840x2160`；当目标分组渠道启用 `features_config.openai_images_upstream.mode=chat_completions` 时，该尺寸也会继续进入转换后的 Chat Completions 提示词，并用于 `generationConfig.imageConfig.aspectRatio`。因此二段提升不会只传“4K”这种模糊指令，避免最终画幅与用户请求的 `size` 不一致。目标分组调度账号时会优先使用管理端配置的 `image_4k_enhancement_model`；未配置时才沿用目标分组自己的可用图片模型解析（例如账号 `model_mapping` 中的 `nano-banana-2`），不要求 Banana 账号额外声明支持源分组的 `gpt-image-2`。

二段提升提示词要求保留原图内容、主体身份、构图、视角、颜色、光照、画幅比例和可见文字，只提升分辨率、锐度、细节和压缩瑕疵。目标分组返回内联图片时，网关会读取最终图片真实像素并写入 Images API 响应的 `data[].size`，用量记录中的 `image_output_size` 也以该字段为准，便于核实二段提升后的实际输出尺寸。目标分组调用失败、不可用或返回无图片时，系统最多尝试 3 次；仍失败则记录日志并返回第一段原图，不向用户暴露二段提升错误。

启用 `4K 提升` 的分组会优先使用目标图片分组做二段提升；未启用该功能时，才按旧配置 `image_super_resolution_enabled` 和网关外部超分服务继续执行原有 4K 超分逻辑。流式图片响应当前不触发图片分组二段提升；当分组已开启 `4K 提升` 时，流式 4K 请求会保留上游原始结果返回，不再回落到旧外部超分。

## Embeddings 请求流程

`POST /v1/embeddings` 要求请求体为合法 JSON，且必须包含非空字符串 `model`。请求通过后端 `OpenAIGatewayHandler.Embeddings` 处理：

1. 从请求上下文读取 API Key、用户和分组信息。
2. 读取并校验请求体，设置运维请求上下文和标准入口 `/v1/embeddings`。
3. 按分组渠道映射解析请求模型，必要时替换请求体中的 `model`。
4. 校验用户并发、账号并发、余额、订阅、API Key 配额和用户平台配额。
5. 使用 OpenAI 调度器选择具备 `embeddings` 能力的 OpenAI 账号。
6. 将请求转发到账号 `base_url` 对应的 `/v1/embeddings`，默认上游为 `https://api.openai.com/v1/embeddings`。
7. 透传上游成功响应，提取 `usage.prompt_tokens`、`usage.input_tokens`、`usage.total_tokens` 等字段用于用量记录。
8. 上游出现可切换账号的错误时，按网关账号切换策略排除失败账号并重试；切换耗尽后返回上游失败错误。

Embeddings 当前为非流式入口。账号缺少 API Key、`base_url` 非法、上游读取超限或上游返回错误时，网关会按 OpenAI 错误结构返回，并记录运维上游错误事件。

## OpenAI 账号能力

OpenAI 账号的 `credentials.openai_endpoint_capabilities` 用于限制账号可承接的 OpenAI 入口能力。

| 能力值 | 说明 |
| --- | --- |
| `chat_completions` | 账号可承接文本类 OpenAI 请求，包括 Chat Completions、Responses 以及内部转换后的文本链路。 |
| `embeddings` | 账号可承接 Embeddings 请求。 |

未配置能力列表时，系统按兼容默认值处理。后台创建和编辑 OpenAI 账号时，界面会提供文本能力和 Embeddings 能力开关，默认同时启用。

## 账号配额自动暂停

OpenAI 账号可根据 Codex 用量窗口自动从调度候选中临时排除。该机制只影响调度选择，不直接修改账号 `status` 或 `schedulable` 字段。

系统读取账号 `extra` 中的用量快照：

| 字段 | 说明 |
| --- | --- |
| `codex_5h_used_percent` | Codex 5 小时窗口使用率，按百分比保存，例如 `95` 表示 95%。 |
| `codex_7d_used_percent` | Codex 7 天窗口使用率，按百分比保存。 |
| `codex_5h_reset_at`、`codex_7d_reset_at` | 对应窗口的绝对重置时间。时间已过期时，旧使用率不会触发暂停。 |
| `codex_5h_reset_after_seconds`、`codex_7d_reset_after_seconds`、`codex_usage_updated_at` | 无绝对重置时间时，用于推算窗口是否已过期。 |

阈值按 `0~1` 的比例保存，后台界面按百分比展示。

| 字段 | 说明 |
| --- | --- |
| `extra.auto_pause_5h_threshold` | 单账号 5 小时窗口自动暂停阈值。 |
| `extra.auto_pause_7d_threshold` | 单账号 7 天窗口自动暂停阈值。 |
| `extra.auto_pause_5h_disabled` | 对当前账号禁用 5 小时窗口自动暂停。 |
| `extra.auto_pause_7d_disabled` | 对当前账号禁用 7 天窗口自动暂停。 |
| `settings.ops_advanced_settings.openai_account_quota_auto_pause.default_threshold_5h` | 全局 5 小时默认阈值。 |
| `settings.ops_advanced_settings.openai_account_quota_auto_pause.default_threshold_7d` | 全局 7 天默认阈值。 |

判断顺序如下：

1. 单账号禁用标记优先级最高；某个窗口被禁用时，该窗口不触发暂停。
2. 单账号阈值大于 `0` 时优先使用单账号阈值。
3. 单账号阈值为空或小于等于 `0` 时，回退到运维高级设置中的全局默认阈值。
4. 有效使用率大于等于阈值时，该账号不会进入本次 OpenAI 调度候选。
5. 用量窗口已重置或没有可用用量快照时，不触发自动暂停。

## 涉及模块

- 网关路由：`backend/internal/server/routes/gateway.go`
- OpenAI Embeddings 处理器：`backend/internal/handler/openai_embeddings.go`
- OpenAI Embeddings 转发服务：`backend/internal/service/openai_embeddings.go`
- OpenAI Images 处理器：`backend/internal/handler/openai_images.go`
- OpenAI Images 原生、4K 提升与 Chat Completions 兼容转发：`backend/internal/service/openai_images.go`、`backend/internal/service/image_4k_enhancement.go`、`backend/internal/service/openai_images_chat_completions.go`
- OpenAI 调度与配额自动暂停：`backend/internal/service/openai_gateway_service.go`、`backend/internal/service/openai_account_scheduler.go`
- 账号创建与编辑界面：`frontend/src/components/account/CreateAccountModal.vue`、`frontend/src/components/account/EditAccountModal.vue`
- 运维高级设置界面：`frontend/src/views/admin/ops/components/OpsSettingsDialog.vue`

### 上游响应档位审计

用量记录的 `service_tier` 是最终计费档位；`upstream_response_service_tier` 独立保存上游响应声明的档位，保留未知值及别名，不从请求档位或计费档位回填。管理端用量表的费用详情同时展示两者。Responses 流式请求以终结事件声明为准，不将 `response.created` 回显的请求档位当成处理结果；非流式 JSON 和 Chat Completions 则读取响应声明。

迁移 245 仅新增可空 TEXT 列，不回填历史数据，不改变 Fast 请求注入或计费规则。字段为空表示历史未记录、上游未声明或观察结果存在冲突，不能解释成 default。OAuth 响应为 default 时仍保留现有计费规则；本字段只供审计，不承诺实际加速效果。回滚应用可保留新增列，无需删除数据或恢复全库。
