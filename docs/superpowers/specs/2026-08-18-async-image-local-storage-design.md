# 异步图片任务复用本地图片存储设计

## 目标

异步图片任务直接复用现有 `generated-images` 本地持久化链路，取消 S3/R2 作为异步功能启用前置条件，避免同一张图片先落本地再重复上传对象存储。

## 方案

- 同步 `POST /v1/images/generations` 行为保持不变。
- 异步请求继续要求最终结果为 URL：未传 `response_format` 时按 `url` 处理，显式传 `b64_json` 时返回 `400 ASYNC_RESPONSE_FORMAT_UNSUPPORTED`。
- Worker 继续调用现有同步图片 Handler；`response_format:url` 会由 `GeneratedImageStore` 保存到 `data/generated-images`，返回 `/generated-images/<filename>` 或已配置的公开 API URL。
- `ImageTaskService.Enabled()` 只依赖 Redis 任务存储可用，不再依赖 S3/R2 resolver。
- 已配置对象存储时仍允许作为可选结果转存；未配置时保留现有本地 URL，不向 Redis 写入 Base64。
- 本地图片目录必须使用持久化挂载；现有清理服务继续按 24 小时策略清理。

## 兼容与边界

- 同步请求、旧异步接口和现有图片 URL 路径不变。
- 异步客户端传 `response_format:url` 无需修改。
- 异步显式请求 `b64_json` 不再接受，避免任务结果膨胀 Redis。
- 单 VPS/单实例继续使用本地存储；未来多实例部署时可打开对象存储转存，不改变任务协议。

## 验证

- 新增异步请求格式测试：默认 URL、显式 URL通过、显式 Base64 拒绝。
- 新增服务启用测试：无 S3 resolver 时任务仍可创建。
- 保留现有同步兼容、任务幂等、查询和对象存储测试。
- staging 验收检查任务结果中的 URL 可访问，且本地图片目录持久化。
