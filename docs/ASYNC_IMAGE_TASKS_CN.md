# 异步图片任务

异步图片任务用于提交耗时较长的 OpenAI 兼容图片请求，并通过轮询获取最终结果。客户端无需维持单个 HTTP 长连接，可避免反向代理或 CDN 因图片生成耗时触发超时。任务执行仍复用同步图片接口既有的路由、计费、内容审核、并发控制和故障切换逻辑。

## 功能入口与前置条件

已认证的 API Key 可使用下列接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/v1/images/generations/async` | 异步文生图 |
| `POST` | `/v1/images/edits/async` | 异步图片编辑或图生图 |
| `GET` | `/v1/images/tasks/{task_id}` | 查询任务状态和最终结果 |

无 `/v1` 前缀的 `/images/generations/async`、`/images/edits/async`、`/images/tasks/{task_id}` 与上述接口语义相同。

功能默认关闭，只有满足以下条件才会启用：

- `image_storage.enabled=true`。
- S3 兼容对象存储的 bucket、访问凭据和必要连接信息完整可用。
- API Key 所属分组为 OpenAI 或 Grok 图片分组，且通过既有图片生成、用户、API Key、IP 和分组校验。

未启用或对象存储配置不完整时，异步接口统一返回 `404`，不会创建 Redis 任务或保存图片结果。流式图片请求不支持异步提交，因为任务轮询只返回一次最终 JSON 结果。

## 对象存储配置

在运行时 `config.yaml` 的 `image_storage` 段配置 S3 兼容对象存储；每个配置项也支持对应的 `IMAGE_STORAGE_*` 环境变量覆盖。Docker 部署会透传 `IMAGE_STORAGE_ENABLED`、`IMAGE_STORAGE_ENDPOINT`、`IMAGE_STORAGE_REGION`、`IMAGE_STORAGE_BUCKET`、`IMAGE_STORAGE_ACCESS_KEY_ID`、`IMAGE_STORAGE_SECRET_ACCESS_KEY`、`IMAGE_STORAGE_PREFIX`、`IMAGE_STORAGE_FORCE_PATH_STYLE`、`IMAGE_STORAGE_PUBLIC_BASE_URL`、`IMAGE_STORAGE_PRESIGN_EXPIRY_HOURS` 与 `IMAGE_STORAGE_MAX_DOWNLOAD_BYTES`。密钥必须保存在 staging 或 prod 的运行时 `.env`、凭据管理工具或配置文件中，禁止写入 Git。

```yaml
image_storage:
  enabled: true
  endpoint: "https://<account_id>.r2.cloudflarestorage.com"
  region: "auto"
  bucket: "<bucket>"
  access_key_id: "<runtime-credential>"
  secret_access_key: "<runtime-credential>"
  prefix: "images/"
  force_path_style: false
  public_base_url: ""
  presign_expiry_hours: 24
  max_download_bytes: 33554432
```

- AWS S3 官方端点可不填写 `endpoint`；Cloudflare R2 通常使用 `region: auto`。
- MinIO 或要求 path-style 访问的桶应设置 `force_path_style: true`。
- `public_base_url` 为空时，结果 URL 为带有效期的预签名链接；填写公开桶或 CDN 前缀时，结果使用该前缀拼接对象 key。
- 当上游仅返回图片 URL 时，服务会下载并转存；`max_download_bytes` 限制该下载内容的最大字节数，默认 32MB。

任务完成后，服务将每张图片上传至对象存储，并把结果重写为 `data[].url`。原始 `b64_json` 不会写入 Redis，避免大图片占满 Redis 内存。上传失败时任务标记为 `failed`，不会保存原始 Base64 结果。

## 提交与轮询流程

提交接口使用与对应同步接口相同的 JSON 或 multipart 请求体。例如：

```bash
curl -i "$BASE_URL/v1/images/generations/async" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-1",
    "prompt": "冬季风暴中的灯塔",
    "size": "1536x1024"
  }'
```

创建成功返回 `202 Accepted`，响应包含 `id`、`task_id`、`status=processing`、创建时间、过期时间和 `poll_url`。响应头中的 `Location` 指向轮询路径，`Retry-After: 3` 表示建议的轮询间隔。

客户端使用提交任务的同一 API Key 访问 `GET /v1/images/tasks/{task_id}`：

- `processing`：任务仍在执行，响应不包含最终图片结果。
- `completed`：`result` 与同步图片接口的最终响应格式一致，但图片已转存为 `data[].url`，`image_url` 复制第一张图片 URL，便于简单客户端使用。
- `failed`：响应包含可用的 OpenAI 兼容错误对象和上游 HTTP 状态。

所有提交和轮询响应均包含 `Cache-Control: no-store`，CDN 不得缓存 `processing` 状态。任务和结果在最近一次状态更新后保留 24 小时，单个任务的最长执行时间为 30 分钟。

## 权限、数据与异常边界

- 任务所有权同时绑定用户 ID 和 API Key ID。不存在的任务与不属于当前 Key 的任务都按不可见任务处理，避免泄露任务是否存在。
- 轮询时仍执行 API Key、用户状态、IP 和分组校验；任务完成后即使该 Key 的余额已经变化，合法任务的轮询仍遵循现有鉴权规则。
- 图片计费、审核、并发与故障切换均发生在后台任务执行阶段，不因改为异步而绕过同步接口的业务约束。
- 对象存储不可用、Redis 不可用、结果上传失败或上游调用失败时，服务返回可区分的任务失败状态或错误响应，不会静默丢弃任务。

## 部署与验证

staging 与 prod 必须分别配置独立的对象存储前缀或 bucket、Redis 和运行时凭据，不能让预发布任务污染正式图片对象或任务记录。

上线验证至少覆盖：

- 未配置对象存储时，三个异步接口返回 `404` 且 Redis 没有新任务。
- 使用允许图片生成的 OpenAI 或 Grok 分组提交任务，确认返回 `202`、`Location`、`Retry-After` 和 `Cache-Control: no-store`。
- 轮询 `processing`、`completed` 与 `failed` 三种状态，确认成功结果只包含对象存储 URL，不包含 `b64_json`。
- 使用不同 API Key 查询同一任务，确认不会暴露任务详情。
- 验证对象存储上传失败时任务进入失败状态，Redis 中不保留大 Base64 图片内容。

## 相关实现

- 路由：`backend/internal/server/routes/gateway.go`
- Handler：`backend/internal/handler/image_task_handler.go`
- 任务服务与 Redis 存储：`backend/internal/service/image_task.go`
- 对象存储与结果转存：`backend/internal/service/image_storage.go`
- 配置示例：`deploy/config.example.yaml`
