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

### 健康状态头记录与替换

OpenAI 账号提供两个独立布尔开关：`extra.openai_healthy_turn_state_record` 缺省为 `true`，`extra.openai_healthy_turn_state_replace` 缺省为 `false`。管理员可先只记录，再按账号启用实验性替换。创建、更新、额外字段更新及批量更新接口均校验字段类型，非法值返回 `400 INVALID_HEALTHY_TURN_STATE_SETTING`。新建账号及未设置记录字段的现有账号自动记录，显式设置 `false` 的账号保持关闭；替换仍需管理员自行开启。默认记录也会启用现有的响应模型一致性检查：响应明确返回其他模型时按失败处理。

记录仅作用于最终上游端点为 `/responses` 的请求，复用请求构造器后切换到图片端点的请求不参与记录或替换。记录来源是 Responses 上游响应的 `X-Codex-Turn-State`，只有收到文本、推理内容或工具参数等有效输出，且响应正常完整结束后才保存。HTTP 200、WS 握手成功、心跳、`response.created` 和空 delta 都不算首字健康。首字后流内失败、异常断流或未正常结束时不保存；替换试验也必须正常结束。JSON 响应须成功完成并有有效输出。记录按账号 ID 和映射后实际发送的模型分别存储、去重和取用，模型仅去除首尾空白，不额外合并大小写或别名。同账号、同模型的 HTTP／WebSocket 和代理出口共用记录，不同账号或模型不能互相取用，失败拒绝摘要也仅在所属账号与模型内生效。上游声明模型与实际发送的模型不一致（例如请求 `gpt-6-astra` 却返回 `gpt-5.6-luna`）也算失败，不保存该状态头；已经用于补试的头计为失败并淘汰。比较复用模型审计规则，以映射后的实际发送模型为准；未声明模型不推断为降级。

HTTP 自动记录与手动采集共用响应格式识别：上游省略 `Content-Type` 时，从已读取的响应前缀识别 SSE 或 JSON，兼容分片、前导空行和 SSE 心跳，格式识别本身不改写响应。开启记录或替换时，在转发前有界检查模型声明，最多缓存 1 MiB；收到模型声明、有效输出或终止事件后继续原样转发。缺少该响应头本身不代表请求不健康；有效输出、正常结束和状态头的要求仍然适用。丢弃或关闭 HTTP 响应时，先取消当前尝试再关闭响应体，避免流读取阻塞；补试使用独立上下文，不取消原始请求。

记录通过 AES-256-GCM 加密保存到 PostgreSQL，服务重启保留。单个头上限 16 KiB，从接收响应头起最长允许使用 40 分钟；同一值重复返回不延长使用期限。这是本地策略，不代表上游承诺的有效期。过期记录不会再被领取，历史计数仍保留；失败头清除密文并保留统计。关闭记录仅停止自动新增，替换开关仍可使用同账号、同模型下尚未过期的旧记录。

账号编辑页的两个开关旁提供“手动获取健康状态头”。默认模型为 `gpt-6-astra`，可填写其他文本模型、选择 HTTP／WebSocket，并选择账号当前代理、直连或已保存的可用代理；一次最多选择 5 个出口，前端依次请求并逐项展示结果，支持取消。每个出口只发送一次固定的 `hi`，最长 60 秒，可在账号或模型冷却期间发送，不占用业务账号并发槽。使用已保存的账号配置，临时出口不写回账号；手动采集不依赖自动记录开关，也不会开启自动替换。只有完整健康响应同时包含有效输出和状态头才提交到正常转发使用的同一持久化存储；429、503、模型不一致、其他错误、空响应、断流、无状态头或取消均不新增记录，也不通过替换或普通重试掩盖失败。已有同值记录不续期。成功记录仅供本账号、相同实际模型使用，可以跨出口和 HTTP／WS 传输方式取用。

管理接口为 `POST /api/v1/admin/accounts/:id/healthy-turn-state/test`，请求体接受 `model`（可省略，默认 `gpt-6-astra`）、`transport`（`http`／`websocket`，默认 `http`）、`proxy_id`（省略或 `null` 使用当前代理，`0` 直连，正数指定现有代理）。权限沿用管理端认证。结果返回状态、实际模型、传输方式、上游 HTTP 状态码及记录过期时间，不返回状态头原值、代理凭据或上游错误正文。

需要大量轮换入口时，在同一面板选择“动态 IP 接口”，保存该账号自己的 HTTPS 提取地址、代理协议、目标新增健康头数量（1–100，默认 5）和最多尝试入口数（1–1000，默认 100，不能低于目标）。服务器从接口提取一批代理，按顺序逐个发送测试，用完后才提取下一批。只将本次成功新增入库的 `recorded` 计入目标，已有记录、无头或失败不增加目标进度，但已发出的测试均占用尝试次数；达到目标或尝试上限即停止。结果仍只归属当前账号和实际发送模型。

提取接口返回换行分隔的公网字面 `IP:端口`（IPv6 使用 `[IP]:端口`），也可返回带 `http://`、`https://`、`socks5://` 或 `socks5h://` 的完整代理地址；不带协议时使用配置中选定的协议。按完整代理地址去重，不把同一主机的不同端口合并。代理入口可能是供应商中转节点，因此“尝试入口数”并不证明实际出口 IP 不同。提取地址的参数原样传递：例如供应商的 `num` 控制每批提取数，`time` 的单位与轮换含义以供应商说明为准，不作为本系统提取间隔。白名单须包含运行 Sub2API 服务的实际出口 IP。

动态提取地址完整加密保存到设置表中的账号专属键，复用 `TOTP_ENCRYPTION_KEY`；读取只返回脱敏地址。地址输入框留空保留已保存值，开始采集前自动保存表单中的动态设置；独立保存不会被账号编辑表单覆盖，也不修改账号的业务代理。提取请求不使用账号代理或环境代理，最长 20 秒、响应最多 64 KiB，不跟随重定向；实际拨号校验公网目标，拒绝内网、回环和元数据地址。临时代理不写入代理表、不回退直连，核心 HTTP／WebSocket 客户端在单次采集后释放，不进入正式请求的共享代理客户端缓存。提取失败会显示明确错误（如 HTTP 403），不把失败伪装成零条成功。供应商即使返回 HTTP 200，正文若为合法 IP 加 `not added to whitelist`，仍作为提取失败，并显示需要加入白名单的服务器出口 IP；其他未知正文不直接回显。连续三批没有新入口时停止，避免无限重复提取。

动态采集管理接口均位于 `/api/v1/admin/accounts/:id/healthy-turn-state/dynamic`，沿用管理员权限：

| 方法与后缀 | 请求与行为 |
| --- | --- |
| `GET /config` | 返回 `configured`、`api_url_masked`、`protocol`、`target_count`、`max_attempts` |
| `PUT /config` | 保存 `api_url`（空值保留）、`protocol`（`http`／`https`／`socks5h`）、`target_count`、`max_attempts` |
| `POST /runs` | 接受 `model`、`transport`，以已保存设置创建运行，不发送探测 |
| `POST /runs/:run_id/step` | 最多提取一批并执行一次探测，返回计数、最近结果和运行状态 |
| `POST /runs/:run_id/stop` | 停止该账号的运行，取消正在执行的提取或探测 |

运行返回 `id`、`status`（`running`／`completed`／`stopped`／`failed`）、`model`、`transport`、`attempts`、`recorded`、`target_count`、`max_attempts`、`fetched_batches`、`last_result` 和提示。前端串行推进，每账号只允许一个运行；关闭弹窗、切换账号、组件卸载或手动停止均停止后续采集，页面没有继续调用时不会在后台自行采集。运行状态仅在当前服务进程中短时保留，空闲 5 分钟或总时长达到 4 小时终止；服务重启后需重新开始，已入库的头和累计数量仍保留。多实例部署应将同一次运行的请求路由到同一实例。

管理员还可调用 `GET /api/v1/admin/accounts/:id/healthy-turn-state` 查询统计。账号编辑页仅展示当前账号累计记录次数及各模型的累计次数，汇总覆盖该账号全部历史记录，不受最近 50 条明细限制。累计次数表示成功入库次数：有效期内相同账号、模型和头值重复返回不重复累计、不续期；记录过期或失败淘汰不扣减历史累计。接口新增 `models` 数组，每项包含 `model`、`captures`、`available`、`in_use`、`attempts`、`successes`、`failures`；原顶层统计字段和 `records`／`probes` 保留，但全部仅属于路径中的账号。明细和手动测试历史各最多 50 项，不再在累计面板展开。失败和无状态头的手动测试也保存结果。接口不返回头原文、密文、摘要、租约标识或凭据；读取失败显示错误，不伪装为空记录。可尝试仅表示未过期且未占用，不代表已证实能恢复请求。成功率为完整成功次数除以已结束的成功与失败次数；仅收到 HTTP 200／WS 101 不计成功。进程中断且未写入结果的调用保持未确认，不计入成功率。

当前持久化结构由迁移 `242_openai_healthy_turn_state_account_model_pool.sql` 创建：`openai_healthy_turn_state_account_model_pool` 保存加密头与累计计数，以 `(account_id, model, value_hash)` 唯一去重；`openai_healthy_turn_state_account_model_rejections` 按相同范围保存失败摘要。`openai_healthy_turn_state_probes` 继续保存按账号隔离的有限手动测试历史。旧 239／240 记录表原样保留，旧全局池缺少账号归属，不能准确拆分，因此不导入新统计，新池从独立采集开始累计；账号删除时其新池记录及拒绝摘要级联删除。迁移不改写账号配置；回滚应用版本不需要删除新表，但旧版会恢复其原有共享池语义。领取采用数据库原子租约，领取后到期前其他请求不能重复领取；完成后释放，失败清除头内容。若进程中断，未决租约保持至头到期。数据库故障时停止本功能的保存／替换，不退回内存；不影响原有转发及错误处理。手动采集没有新增冷却或业务并发门禁。

加密复用 `TOTP_ENCRYPTION_KEY`，必须在运行时配置中稳定保存，重启和多个实例使用同一密钥。更换密钥会使旧头不可解密，领取时停用该记录，历史统计仍保留；不得把密钥或状态头写入仓库与日志。迁移仅新增独立表，旧版镜像回滚可保留这些表；不用回滚账号表。批量关闭开关通过管理员账号批量更新接口合并这两个 `extra` 键并刷新调度快照；操作前只在受限运行目录保存账号 ID、两个键是否存在与原值，恢复时也只恢复这两个键，不覆盖其他配置或凭据。

迁移 `243_openai_healthy_turn_state_temporary_proxy.sql` 给手动测试历史增加 `temporary_proxy`，标识没有持久代理 ID 的动态入口，避免将其解释为直连；旧测试记录默认为 `false`。迁移只追加字段，回滚应用可保留。取消会中止尚在进行的请求并停止后续出口测试；取消前已完成并提交的健康记录仍保留至正常过期。

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

健康状态头的记录、领取、租约、失败淘汰和累计统计均按账号与实际发送模型隔离。同账号、同模型可跨 HTTP／WebSocket 和代理使用，记录默认开启，替换默认关闭。采集条件、计数口径、接口和历史数据处理见[健康状态头记录与替换](#健康状态头记录与替换)。

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
