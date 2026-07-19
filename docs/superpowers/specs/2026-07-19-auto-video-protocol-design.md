# OpenAI 分组自动视频协议设计

## 背景与实测结论

当前 OpenAI 分组的视频能力只对 `dreamina-seedance-*` 模型开放，并把统一视频请求转换为
`/v1/chat/completions`。该实现能够兼容部分 Helix Seedance 响应，但无法覆盖采用标准异步
视频任务接口的聚合上游。

2026-07-19 使用正式环境现有账号完成了两组最低成本真实探测：

- `视频`（Helix）支持 `POST /v1/videos`，`dreamina-seedance-2-0-mini-ep` 能从
  `queued`、`in_progress` 进入 `completed`，并支持
  `GET /v1/videos/{task_id}/content` 下载 MP4。
- `视频2`（Skylee）支持 `POST /v1/videos`，`jing-video-2-pro` 能完成相同异步任务流程。
- 两个上游都接受字符串形式的 `seconds`；Helix 不接受数字形式的 `seconds`。
- Helix 的完成结果 URL 位于 `metadata.url`，Skylee 会返回自己的结果 URL；两个上游都应由
  Sub2API 的内容代理统一下载，避免向客户端暴露上游临时签名地址。

因此，OpenAI 分组视频能力应从“按 Seedance 模型名进入 Chat Completions 特判”改成“按视频
接口意图进入通用异步视频网关，并自动发现上游协议”。

## 目标

- 对外提供 `POST /v1/videos` 作为统一视频任务创建端点。
- 保留 `POST /v1/videos/generations`，兼容现有客户端和测试台历史调用。
- OpenAI API Key 账号默认优先使用上游 `/v1/videos` 异步协议。
- 仅在明确证明上游不支持 `/v1/videos` 且没有创建任务时，回退现有 Chat Completions 视频协议。
- 协议选择不依赖 Seedance、Jing、Kling、Veo、Sora 等模型名硬编码。
- 使用现有账号模型映射，把统一对外模型映射为各供应商实际模型名。
- 异步状态查询和内容下载始终回到创建任务的同一账号。
- 测试台在视频模式下按接口意图调用视频端点，不要求前端先识别模型名称。
- 创建失败不扣费；任务创建成功只扣费一次；轮询和内容下载不扣费。

## 非目标

- 不自动判断不同供应商宣传名称是否属于同一个真实底模。
- 不自动创建账号模型映射；模型映射仍由账号配置或现有同步能力提供。
- 不承诺对上游异步任务失败自动退款。供应商是否退款不可从统一接口可靠获知。
- 不移除现有 Grok 原生视频协议；Grok 路由和资格校验继续保持独立。
- 不在本次改造中新增视频编辑、延长或视频输入能力。

## 方案比较

### 按模型名硬编码协议

根据 `seedance`、`jing-video` 等名称选择协议。实现简单，但每次新增或改名都需要发版，且模型
别名无法证明真实协议。该方案不采用。

### 每个账号手工配置协议

为账号选择 `videos` 或 `chat_completions`。行为确定，但新账号需要人工判断和维护，不符合自动
接入大量视频模型的目标。只保留未来应急覆盖的扩展空间，本次不增加日常配置项。

### 自动端点优先与能力缓存

所有 OpenAI 分组视频请求优先访问上游 `/v1/videos`。成功响应或正常业务参数错误都能证明端点
存在；只有明确的端点不存在响应才回退 Chat Completions。探测结果按账号和映射后模型缓存。

本方案不依赖模型名，能够覆盖 Helix、Skylee 以及未来采用相同异步接口的供应商，因此采用。

## 外部 API

### 创建任务

以下两个入口行为一致：

```text
POST /v1/videos
POST /v1/videos/generations
```

统一请求字段：

```json
{
  "model": "dreamina-seedance-2-0-ep",
  "prompt": "电影感镜头，雨夜霓虹街道",
  "resolution": "720p",
  "duration": 5,
  "image_urls": ["https://example.com/reference.png"]
}
```

兼容读取现有 `image.url`、`reference_images[].url` 和 `reference_image_urls`。转发上游时统一使用：

```json
{
  "model": "账号映射后的模型",
  "prompt": "电影感镜头，雨夜霓虹街道",
  "resolution": "720p",
  "seconds": "5",
  "image_urls": ["https://example.com/reference.png"]
}
```

`seconds` 使用字符串，以同时兼容已验证的 Helix 和 Skylee。

统一创建响应：

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "model": "dreamina-seedance-2-0-ep",
  "status": "queued",
  "progress": 0
}
```

### 查询任务

```text
GET /v1/videos/{task_id}
```

状态统一为：

- `queued`
- `in_progress`
- `completed`
- `failed`

完成响应中的视频地址统一指向本站：

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "model": "dreamina-seedance-2-0-ep",
  "status": "completed",
  "progress": 100,
  "url": "/v1/videos/task_xxx/content"
}
```

### 下载内容

```text
GET /v1/videos/{task_id}/content
```

内容端点使用任务绑定账号访问上游 `/v1/videos/{task_id}/content`。透传安全的媒体响应头和
`Range`，支持浏览器播放与拖动，不把账号凭据或上游签名 URL返回给客户端。

## 自动协议发现

协议缓存键由以下信息组成：

```text
account_id + mapped_model
```

状态只有：

- `videos`：上游支持 `/v1/videos`。
- `chat_completions`：上游明确不支持 `/v1/videos`，使用旧同步兼容协议。

首次请求或缓存失效时：

1. 使用真实用户请求调用上游 `/v1/videos`，不额外创建探测任务。
2. `2xx` 且包含任务 ID：记录 `videos`，绑定任务并返回。
3. `400` 业务校验错误，例如缺少提示词、字段格式或模型参数错误：记录 `videos`，原样返回规范化
   业务错误，不回退。
4. `401`、`403`、`429`：视为账号鉴权、权限或额度问题，不改变协议缓存，不回退。
5. `5xx`、超时、断线：协议状态保持未知，不回退。上游可能已创建任务，回退会导致重复生成。
6. 只有 `404`、`405` 或明确错误码/消息表示 endpoint unsupported/not found 时，记录
   `chat_completions` 并在同一请求中执行一次旧协议。
7. 已缓存 `videos` 的账号若后来明确返回端点不存在，清除旧缓存并进入回退。

缓存使用 Redis，并设置有限 TTL。账号凭据、base URL 或模型映射变更时，应通过现有账号缓存刷新
路径同步使协议缓存失效。缓存故障时退化为每次优先尝试 `/v1/videos`，不影响正确性。

## 调度与任务绑定

创建任务继续复用 OpenAI 平台现有账号调度器：

1. 使用客户端请求模型进行分组和账号筛选。
2. 选择账号后应用账号 `model_mapping`，得到实际上游模型。
3. 协议发现以账号 ID 和实际上游模型为维度，允许同一供应商的不同模型采用不同协议。
4. `/v1/videos` 创建成功后，将以下所有权绑定写入 Redis：

```text
user_id + api_key_id + group_id + task_id -> account_id
```

5. 状态和内容请求必须解析到同一绑定账号。绑定不存在、用户不一致或 API Key 不一致时统一返回
   404，避免泄露任务是否存在。
6. 查询和下载不得在其他账号间故障切换，因为上游任务只存在于创建账号中。

现有 Grok 视频任务绑定逻辑应抽取为平台无关的视频任务绑定能力，Grok 和 OpenAI 异步视频共同
使用，避免维护两套安全所有权规则。

## 响应规范化

创建任务 ID 兼容读取：

- `task_id`
- `id`
- `request_id`
- `data.task_id`
- `data.id`

状态兼容读取并规范化大小写和别名：

- `queued`、`pending` -> `queued`
- `in_progress`、`processing`、`running` -> `in_progress`
- `completed`、`succeeded`、`success`、`done` -> `completed`
- `failed`、`error`、`cancelled`、`canceled` -> `failed`

进度兼容数字和百分数字符串，并限制在 `0-100`。上游 URL 兼容读取
`metadata.url`、`video_url`、`result_url`、`url`、`video_urls[0]` 和 `videos[0].url`，但对客户端
只返回本站内容代理地址。

## Chat Completions 回退

旧协议仅服务于明确不支持 `/v1/videos` 的 OpenAI 兼容上游：

- 保留当前文本和参考图片消息转换。
- 只接受同步返回最终 HTTPS 视频 URL 的响应。
- 如果只返回任务 ID 而没有通用状态端点信息，则返回明确的上游协议不完整错误，不伪造成功。
- 回退成功后按同步完成视频记录用量。

当前 Helix 和 Skylee 已验证支持 `/v1/videos`，正常情况下不会进入此回退。

## 计费语义

- 上游创建响应为 `2xx` 且取得任务 ID 后，记录一次视频用量。
- 记录请求模型、映射后模型、账号、分组、分辨率、时长和参考图片数量。
- 轮询、内容下载以及重复读取完成状态不记录用量。
- 创建请求在取得任务 ID 前失败时不扣费。
- 协议探测不产生独立请求，因此不存在额外探测费用。
- 异步任务后来失败时不自动退款。供应商是否退款无法从统一协议可靠确认；未来如增加退款能力，
  必须以可审计的任务状态和幂等账务记录单独设计。

## 模型测试台

测试台不再要求前端根据模型名判断是否为视频：

- 用户选择“视频”模式时，当前 API Key 的 `/v1/models` 模型均可被选择和搜索。
- 视频模式始终调用 `POST /v1/videos`。
- 文本和图片模式继续使用已有分类和端点，不受影响。
- 创建任务后按 `task_id` 每 5 秒轮询，设置最大次数并允许用户取消。
- `completed` 后通过本站 `/v1/videos/{task_id}/content` 获取 Blob 并播放。
- `failed` 显示规范化错误；超时保留任务 ID，允许用户重新查询，绝不重新创建任务。
- 已配置视频计费的模型仍显示价格预览；没有定价的未知模型显示价格未知，不阻止测试。

这样新增视频模型只需出现在账号的模型列表或模型映射中，无需前端发布新的名称判断规则。

## 错误处理

- 请求缺少模型或提示词：`400 invalid_request_error`。
- 未找到支持请求模型的账号：沿用现有 OpenAI 无可用账号错误。
- 上游明确不支持异步端点且 Chat 回退失败：返回最后一个可解释的上游错误。
- 创建响应没有任务 ID：`502 upstream_error`，不记录用量。
- 任务绑定不存在或所有权不匹配：`404 not_found_error`。
- 上游状态异常或结构不可解析：`502 upstream_error`，保留任务绑定供后续重试。
- 内容不是允许的视频类型、响应过大或发生不安全重定向：拒绝代理并记录运维错误。
- 日志不得记录 API Key、完整上游签名 URL、完整提示词或参考图 data URL。

## 测试范围

### 后端单元测试

- 通用视频请求解析和 `seconds` 字符串转换。
- 账号模型映射发生在协议缓存和上游转发之前。
- `/v1/videos` 成功创建任务并规范化响应。
- Helix `metadata.url` 和 Skylee 常见 URL 字段解析。
- 业务 `400` 不回退，`401/403/429/5xx/超时` 不回退。
- 明确 `404/405/unsupported endpoint` 时只回退一次 Chat Completions。
- 不在不确定错误后创建第二个任务。
- 任务所有权绑定隔离用户、API Key 和分组。
- 状态规范化、进度解析和完成内容 URL 改写。
- 内容代理鉴权、Range、Content-Type、体积限制和重定向限制。
- 创建成功只记录一次视频用量，轮询和下载不计费。
- Grok 视频现有行为不回归。

### 前端单元测试

- 视频模式可以选择当前 Key 返回的未知模型名。
- 视频模式调用 `/v1/videos` 而不是 Chat Completions。
- 创建后只轮询同一任务，不重复创建。
- `queued`、`in_progress`、`completed`、`failed` 状态展示。
- 完成后从本站内容端点读取 Blob。
- 轮询超时和用户取消保留任务 ID。
- 未配置价格的模型仍可测试，并显示价格未知。

### 集成和 staging 验证

- 使用正式 VPS 隔离 staging，不触碰 prod 数据。
- `视频` 账号：选择 Mini、480p、4 秒，验证 Helix `/v1/videos` 创建、轮询和内容下载。
- `视频2` 账号：选择映射到 `jing-video-2-pro` 的统一模型，验证 Skylee 完整流程。
- 验证两个任务分别绑定原创建账号，互不串号。
- 验证使用其他用户或 API Key 查询任务返回 404。
- 验证每个创建任务只有一条视频用量记录，轮询和下载没有新增计费记录。
- 检查 staging 容器日志不包含密钥、签名 URL 或完整提示词。

## 发布边界

本次代码先提交并推送功能分支，在正式 VPS 使用独立 staging compose、数据库、Redis、数据目录和
`18080` 端口验证。staging 通过后只报告结果和风险；必须等待用户明确口头确认，才允许进入
`dev/main` 合并和 prod 切换。
