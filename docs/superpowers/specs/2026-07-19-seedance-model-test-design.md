# Seedance 模型测试台支持设计

## 背景与根因

线上 OpenAI 分组 `视频测试` 已绑定 Seedance API Key 账号，上游 `/v1/models` 能返回
`dreamina-seedance-2-0-*` 系列模型，但该分组没有绑定渠道。

当前模型测试台只根据 `/api/v1/channels/available` 生成分组和模型候选，因此无渠道分组不会出现。
此外，前端只识别 `grok-imagine-video*` 为视频模型，后端视频路由也只允许 Grok 平台。
Helix 上游没有实现 `/v1/videos/generations`，Seedance 需要通过其 OpenAI 兼容
`/v1/chat/completions` 协议调用。

## 目标

- 无渠道的 API Key 分组也能在模型测试台读取真实 `/v1/models` 模型。
- `dreamina-seedance-2-0-*` 在测试台归类为视频模型。
- 测试台能通过 Seedance 专用适配器完成生成并展示视频。
- 未配置分组或渠道视频价格时，使用 BytePlus 官方默认价格计费。
- 已配置的分组价格、渠道价格和用户倍率继续优先于系统默认价格。
- 不改变 Grok 视频生成、轮询和内容下载行为。

## 非目标

- 不把所有 OpenAI API Key 账号都开放为通用视频账号。
- 不修改 Helix 账号凭据、分组绑定或渠道配置。
- 不实现视频输入；本次只覆盖测试台已有的文本和图片参考输入。
- 不新增 4K 选项。现有系统视频计费字段和测试台只支持 480p、720p、1080p。

## 官方默认价格

价格来源为 BytePlus ModelArk `Pricing` 页面的视频生成价格表，页面更新时间为
2026-07-13。采用无视频输入、16:9 的官方按秒估算价：

| 模型系列 | 480p | 720p | 1080p |
| --- | ---: | ---: | ---: |
| Dreamina Seedance 2.0 | 0.07 USD/秒 | 0.15 USD/秒 | 0.37 USD/秒 |
| Dreamina Seedance 2.0 Fast | 0.06 USD/秒 | 0.12 USD/秒 | 不支持 |
| Dreamina Seedance 2.0 Mini / Mini HC | 0.04 USD/秒 | 0.08 USD/秒 | 不支持 |

官方文档：<https://docs.byteplus.com/en/docs/ModelArk/1099320#video-generation>

模型映射：

- `dreamina-seedance-2-0-ep` 使用 Seedance 2.0 价格。
- `dreamina-seedance-2-0-fast-ep` 使用 Seedance 2.0 Fast 价格。
- `dreamina-seedance-2-0-mini-ep` 和 `dreamina-seedance-2-0-mini-hc` 使用 Mini 价格。

## 模型发现

模型测试台仍使用 `/api/v1/channels/available` 获取渠道价格和渠道分组模型。加载 API Key 后，
对当前选中的 API Key 调用网关 `/v1/models`，把返回模型合并到当前 Key 的分组：

- 渠道模型优先保留其定价对象。
- `/v1/models` 中新增的模型使用 `pricing: null`，由系统默认价格负责展示和计费。
- 切换 API Key 时重新读取该 Key 的模型；结果按 Key 缓存，避免重复请求。
- 请求失败时保留渠道模型并显示现有错误提示，不影响其他分组使用。

## 模型分类和参数

- `dreamina-seedance-2-0-*` 统一识别为 `video`。
- 完整版支持 480p、720p、1080p。
- Fast、Mini、Mini HC 只提供 480p、720p。
- 时长继续使用测试台现有 4 到 15 秒约束。
- Seedance 参考图片沿用测试台现有 data URL 输入；适配器负责转换为上游接受的消息内容。

## Seedance 上游适配

新增显式的 Seedance 模型判断，不放宽所有 OpenAI 平台的视频路由。

测试台调用保持 `/v1/videos/generations` 和 `/v1/videos/:id` 的统一内部协议。后端检测到
OpenAI 分组和 Seedance 模型时：

1. 选择支持该模型的 OpenAI API Key 账号。
2. 将视频请求转换为 Helix `/v1/chat/completions` 请求。
3. 透传账号鉴权、base URL、模型映射和现有调度能力。
4. 解析上游响应中的任务 ID、状态、视频 URL 和用量。
5. 若上游同步返回最终视频，直接返回完成结果；若返回任务 ID，则记录任务与账号关系，后续状态查询继续访问同一账号。
6. 上游错误保持现有错误透传和账号调度语义，不泄露凭据或完整上游响应。

实现前允许执行一次 Mini、480p、4 秒的最低成本生成探测，用来确认 Helix 请求和响应结构。
探测结果只记录必要字段，不记录 API Key、完整提示词或敏感响应。

## 计费

计费优先级保持现有顺序：

1. 分组指定分辨率视频价格。
2. 渠道 `video` 定价区间或默认价格。
3. Seedance 官方系统默认价格。

成功生成按 `每秒价格 × 输出时长 × 视频数量 × 有效倍率` 扣费。失败、审核拒绝或没有生成结果时
不记录成功视频用量。若上游返回实际时长，以实际时长为准；否则使用请求时长。

## 测试

- 前端单元测试：Seedance 分类、支持分辨率、官方价格、API Key 模型合并与缓存。
- 后端单元测试：各 Seedance 型号默认价格、OpenAI Seedance 路由、请求转换、同步响应、异步响应、
  状态查询、错误透传和成功计费。
- 回归测试：Grok 视频路由仍走原实现；普通 OpenAI 模型仍拒绝视频端点。
- 构建验证：后端完整单元测试、前端测试和生产构建。

## 发布与回滚

- 在独立功能分支提交并推送。
- 正式 VPS 从该分支构建带 commit 的 staging 镜像。
- 使用线上 `视频测试` API Key 验证模型列表、价格预览和一次最低成本生成。
- staging 通过后合并到 `main`，prod 复用同一镜像内容切换。
- 保留当前生产镜像作为回滚点；数据库不需要迁移。
