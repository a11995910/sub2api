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

### 健康状态头采集与替换

OpenAI OAuth 账号编辑页只保留“异常时替换健康状态头（实验）”，对应 `extra.openai_healthy_turn_state_replace`，默认关闭。开启后使用全局共享的动态 IP 接口、本账号勾选模型及目标库存维护健康头；普通业务响应不再自动记录健康头。旧 `extra.openai_healthy_turn_state_record` 不再生效，创建、更新、额外字段更新和批量更新校验时会移除该旧字段；替换开关必须为布尔值，否则返回 `400 INVALID_HEALTHY_TURN_STATE_SETTING`。

编辑页从账号上游同步模型并以复选框展示，必须至少勾选一个文本模型，不预选猜测模型。管理员专用 `GET /api/v1/admin/accounts/:id/healthy-turn-state/models` 返回 `[{id, display_name}]`；OAuth 复用带账号认证的 `https://chatgpt.com/backend-api/codex/models` 与现有账号模型缓存。清单过滤图片等专用媒体模型和通配符，并核实账号映射后的实际模型仍在上游目录中；有效的账号精确别名可以选择。上游同步失败明确报错，不回退静态默认目录。保存动态配置时再次校验所选模型。静态 OpenAI 模型目录与官方上游保持一致，健康头实际可选项以账号上游清单为准。

健康头目标为每个勾选模型的有效库存，范围 1–100，默认 3。同账号多个别名映射到同一个实际模型时共用这一份目标；例如两个别名都指向 `gpt-6-astra`，只维护 3 个而不是 6 个。不同实际模型独立维护，选择两个实际模型且目标为 3 时，最多维护各 3 个。有效且正在使用的头也计入库存，不会因为临时借出而额外采集。已有有效库存达到目标后，不再提取代理或发送测试请求；使用失败或到期清除后，仅补齐缺口。降低目标不会主动删除仍有效的多余记录，等待使用淘汰或到期后自然收敛。

自动维护由服务端负责，关闭弹窗、切换账号和浏览器离线均不影响运行，服务重启后从保存的配置恢复。扫描间隔为 15 秒，同一进程最多同时维护 4 个账号；只处理有效、已开启替换、已勾选模型且全局提取接口可用的 OpenAI OAuth 类账号。旧配置没有勾选模型时不自动发送请求。每次继续采集前重新检查开关、账号和配置；关闭替换后停止后续补位。连续健康采集间隔至少 1 秒，失败按 30 秒起步指数退避，最长 10 分钟。一次补位运行还受最多尝试入口数限制，默认 100，范围 1–1000，不能低于目标库存。失败、无头、已有同值头不增加有效库存；同一值重复返回不延长原有效期。

采集仅使用动态 IP 接口返回的临时代理，不使用直连或账号业务代理作为采集回退。全局保存一次 HTTPS 提取地址和代理协议后，所有账号复用该配置，服务端按批提取代理并逐个发送一次固定的 `hi`。只有 5 秒内收到有效文本、推理或工具参数输出，且响应完整成功并包含有效 `X-Codex-Turn-State` 时才保存；单次请求最长 60 秒。HTTP 200、WS 握手成功、心跳、`response.created` 和空 delta 不算有效输出；429、503、模型不一致、空响应、断流、首字超时、无状态头或取消均不新增记录，也不会通过替换或普通请求重试掩盖失败。采集可在账号或模型冷却期间发送，不占用业务账号并发槽，不执行普通账号测试的状态恢复。

采集与业务替换复用响应格式识别：上游省略 `Content-Type` 时，从已读取的响应前缀识别 SSE 或 JSON，兼容分片、前导空行和 SSE 心跳，格式识别本身不改写响应。Responses 转发在有界模型检查后继续传递响应，最多缓存 1 MiB；上游声明模型与实际发送模型不一致时按失败处理。模型比较复用审计规则，以映射后实际发送模型为准；未声明模型不推断为降级。仅最终上游端点为 `/responses` 的请求参与，复用请求构造器后切换到图片端点的请求除外。

记录按账号 ID 和映射后实际模型隔离，模型仅去除首尾空白，不额外合并大小写。同账号、同模型的 HTTP／WebSocket 与不同代理出口共用库存；不同账号或实际模型不能互相取用。头通过 AES-256-GCM 加密保存到 PostgreSQL，重启保留，单个头最大 16 KiB；从采集响应头起最长使用 40 分钟，这是本地策略，不代表上游承诺的有效期。过期或使用失败时清除可用密文与租约，保留历史统计；过期拒绝摘要同步清理。替换成功会归还原头继续使用，不新增业务响应中的头，也不延长寿命。数据库故障时停止保存和替换，不回退内存池，不影响原有转发错误处理。

提取接口返回换行分隔的公网字面 `IP:端口`（IPv6 使用 `[IP]:端口`），也可返回带 `http://`、`https://`、`socks5://` 或 `socks5h://` 的完整代理地址；不带协议时使用配置中选定的协议。按完整地址去重，同主机的不同端口不合并。代理入口可能是供应商中转节点，尝试入口数不证明实际出口 IP 不同。提取地址参数原样传递：供应商的批量数和轮转时间含义由供应商定义，不作为本系统采集间隔。供应商白名单须包含运行 Sub2API 服务的实际出口 IP。

完整提取地址加密保存到设置表中的全局配置，复用 `TOTP_ENCRYPTION_KEY`；管理接口只返回脱敏地址。提取 URL 和代理协议由所有账号共享，配置一次后其他账号无需重复填写；输入留空保留全局地址，填写新地址或修改代理协议会影响所有账号。模型、目标库存、每轮尝试上限和 HTTP／WebSocket 传输方式仍按账号独立保存。编辑页通过底部“更新”先保存采集配置，再保存账号及替换开关；采集配置使用独立管理接口存储，不修改业务代理。提取请求不使用账号代理或环境代理，最长 20 秒、响应最多 64 KiB、不跟随重定向；拨号拒绝内网、回环和元数据地址。临时代理不写入代理表、不回退直连，HTTP／WebSocket 客户端在单次采集后释放，不进入正式请求的共享代理缓存。提取失败保留明确错误，例如 HTTP 403 或白名单缺失；未知供应商正文不直接回显。连续三批没有新入口时停止本轮采集。

首次读取共享配置时，若历史账号的有效提取地址和代理协议一致，则自动沿用并加密保存为全局配置；若存在不同配置，管理接口返回 `shared_proxy_conflict: true` 并保留账号采集选项，管理员填写一次统一地址后恢复共享，冲突期间不执行采集。历史账号加密副本保留用于旧版本回滚；回滚后按旧版本读取各账号副本，其他账号可能恢复之前的地址。发布后应刷新已打开的编辑页面，使账号设置保存携带共享配置更新标记。

动态采集管理接口均位于 `/api/v1/admin/accounts/:id/healthy-turn-state/dynamic`，沿用管理员权限：

| 方法与后缀 | 请求与行为 |
| --- | --- |
| `GET /config` | 返回全局 `configured`、`api_url_masked`、`protocol` 及账号独立的 `target_count`、`max_attempts`、`models`、`transport` |
| `PUT /config` | 保存全局 `api_url`（空值保留）、`protocol`（`http`／`https`／`socks5h`），以及账号独立的 `target_count`、`max_attempts`、勾选的 `models`、`transport`（`http`／`websocket`）；编辑页仅在地址或协议被修改时发送 `update_shared_proxy: true`，否则发送 `false`，避免旧弹窗覆盖其他账号新保存的全局设置 |
| `POST /runs` | 使用保存的勾选模型和采集设置创建立即补位运行，兼容旧 `model`、`transport` 请求字段 |
| `POST /runs/:run_id/step` | 检查现有库存，未满时最多提取一批并执行一次探测；满额时不发网络请求 |
| `POST /runs/:run_id/stop` | 停止该账号本次手动运行，取消正在执行的提取或探测；持续维护由替换开关控制 |

运行返回 `id`、`status`（`running`／`completed`／`stopped`／`failed`）、当前实际 `model`、选择的 `models`、`transport`、`attempts`、`recorded`、`target_count`、`max_attempts`、`fetched_batches`、`last_result` 和提示。`recorded` 表示本次新增数，目标判断使用数据库中的当前有效库存。编辑页使用服务端自动维护；上述手动运行接口保留兼容，需调用方逐步推进，停止仅影响本次运行，已开启的服务端维护仍继续。每账号同一进程只允许一个采集运行，避免后台维护与手动补位重复发出测试。手动运行空闲 5 分钟或总时长达到 4 小时终止；运行状态仅短时保留，已保存头与历史统计独立持久化。当前生产部署为单实例，采集运行协调仍为进程内状态。

管理员还可调用 `GET /api/v1/admin/accounts/:id/healthy-turn-state` 查询当前有效库存、占用数量和历史累计。每模型统计包含 `model`、`captures`、`available`、`in_use`、`attempts`、`successes`、`failures`，有效库存为 `available + in_use`。`maintenance` 返回后台维护状态、脱敏结果提示与下次重试时间，提取 403、白名单失败或探测失败可在面板中查看；面板活跃时每 15 秒刷新。累计次数不受最近 50 条明细限制，过期或淘汰不扣减历史累计；有效期内同值返回不会重复计数。接口保留顶层统计和最近的 `records`／`probes`，全部仅属于路径账号；不返回头原文、密文、摘要、租约或凭据。成功率只计算完整结束的替换使用；HTTP 200／WS 101 本身不算成功，进程中断且未写入结果的调用保持未确认。

迁移 `242_openai_healthy_turn_state_account_model_pool.sql` 创建的账号模型池按 `(account_id, model, value_hash)` 去重；拒绝摘要保持相同范围，账号删除时级联清理。旧全局池没有准确账号归属，保留但不导入新池。迁移 `243_openai_healthy_turn_state_temporary_proxy.sql` 通过 `temporary_proxy` 标识动态入口，避免解释为直连。领取采用数据库原子租约，完成后释放，失败清除头内容；未决租约保留至头到期。加密密钥需跨重启保持一致；更换密钥会使旧头不可解密并在领取时停用，历史统计仍保留。状态头、密钥和提取凭据不得写入日志或仓库。本次库存维护调整不新增数据表或迁移。

旧 `POST /api/v1/admin/accounts/:id/healthy-turn-state/test` 已移除，采集入口统一使用有库存检查的动态 IP 运行，避免直连或固定代理单次测试绕过库存上限。

替换触发于 Responses HTTP 请求或 WS 握手实际返回的 `429`／`503`，每个客户端请求（长连接为当前 WS 会话）每个账号最多补试一次。HTTP 使用相同请求体，只修改该次重试的状态头；WS 强制建立新上游连接，严格依赖原连接的续链不做替换。相同状态、无记录、已过期或预算已用尽时保持原有处理。完整遵守 `Retry-After`，最少等待一秒；等待达到两分钟或超出请求剩余时间时跳过试验。记录领取期间不会被其他请求重复取用，未发出补试前取消会归还记录。

补试仍遇到 HTTP 错误、传输错误、空响应、流内失败、首字超时或异常 EOF 时淘汰已用记录，并在数据库保留其散列 40 分钟，拒绝再次回填，避免并发旧响应把失效状态重新放回。只记录操作类型、账号 ID、模型和状态码，不输出状态值。淘汰后继续既有错误处理、退避及账号切换；不会清除账号额度或冷却记录。

流内错误会淘汰试验记录，但 HTTP 200／WS 101 后的流内 `429`／`503` 不在本功能中重新播放；已向客户端输出内容的请求也不回放。compact、搜索、用量和普通 Chat Completions 上游不参与，Messages／Chat Completions 经 Responses 桥接时参与。此功能不保证解除限流、过载或改善回答质量，需结合实际错误码和成功率评估。

### 常规退避

OpenAI OAuth / SetupToken 的瞬时 `429` 使用原有的 2 分钟同账号重试窗口，不额外设置固定 3 次上限。普通重试默认间隔 500 毫秒；上游 `Retry-After` 指定等待时，最多等待 8 秒，并按剩余窗口截断。窗口结束后进入账号切换或错误处理。其他错误继续使用各自的次数限制和退避规则。健康状态头替换只补试一次，其等待规则见上文。

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

## OpenAI 健康状态头隔离与补试

健康状态头的采集、领取、租约、失败淘汰和累计统计均按账号与实际发送模型隔离。同账号、同模型可跨 HTTP／WebSocket 和代理使用；替换默认关闭，开启后按已保存的动态 IP 接口与勾选模型维护有效库存，普通请求不新增记录。采集条件、计数口径、接口和历史数据处理见[健康状态头采集与替换](#健康状态头采集与替换)。

遇到 HTTP 请求或 WebSocket 握手 `429`／`503`，或响应声明模型与实际发送模型不一致时，开启替换的请求最多从当前账号、当前模型领取一条未过期记录补试。三类异常共享同一次补试额度，并遵守上游等待时间。模型不一致仅在响应尚未转发、原请求可安全重放时补试；WS 使用新连接和原始请求帧，包含 `previous_response_id` 或要求严格复用原连接时不重放。没有可用记录、替换关闭或补试仍不一致时，HTTP 返回 `502 upstream_model_mismatch`；WS 返回转发错误。已开始转发的流若后续才声明不一致，终止流并记为失败，不重放已经交付的内容。数据库租约保证同一条记录不会同时被多个请求领取；补试成功后归还，失败时清除密文并保存当前账号与模型下的拒绝摘要。

## 涉及模块

- 网关路由：`backend/internal/server/routes/gateway.go`
- OpenAI Embeddings 处理器：`backend/internal/handler/openai_embeddings.go`
- OpenAI Embeddings 转发服务：`backend/internal/service/openai_embeddings.go`
- OpenAI Images 处理器：`backend/internal/handler/openai_images.go`
- OpenAI Images 原生、4K 提升与 Chat Completions 兼容转发：`backend/internal/service/openai_images.go`、`backend/internal/service/image_4k_enhancement.go`、`backend/internal/service/openai_images_chat_completions.go`
- OpenAI 调度与配额自动暂停：`backend/internal/service/openai_gateway_service.go`、`backend/internal/service/openai_account_scheduler.go`
- 账号创建与编辑界面：`frontend/src/components/account/CreateAccountModal.vue`、`frontend/src/components/account/EditAccountModal.vue`
- 运维高级设置界面：`frontend/src/views/admin/ops/components/OpsSettingsDialog.vue`
