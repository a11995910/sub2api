# 开放 API 开发文档

> 本文档可直接提供给 AI，用于生成文本、图片和视频 API 的接入代码。

公开网页：`/developer-docs`

AI 纯文本版：`/developer-docs.md`

## 给 AI 的实现要求

如果你正在根据本文档生成代码，请遵守以下要求：

1. 先确认用户的 API Base URL、API Key、目标模型、编程语言和运行环境。
2. API Key 必须通过服务端环境变量或密钥管理服务读取，不得写进浏览器前端、仓库、截图或日志。
3. 所有请求都要设置超时，并检查 HTTP 状态码和 `error.message`。
4. 对 `429` 和临时 `5xx` 使用带抖动的指数退避，不得无限重试。
5. 图片客户端必须同时支持 `data[].b64_json` 和 `data[].url`。
6. URL 图片默认只保留 24 小时，业务需要长期保存时必须及时下载并转存。
7. 视频必须按“创建任务、轮询状态、下载 content”三步实现。
8. 不要让客户端绕过本站 content 接口直接依赖视频上游 CDN 地址。

## 1. 基础配置

接口地址优先使用平台“使用密钥”弹窗提供的 Base URL。没有单独配置时，通常与本文档所在站点同源。

```bash
export BASE_URL="https://api.example.com"
export API_KEY="sk-your-api-key"
```

所有 API 请求使用 Bearer 鉴权：

```http
Authorization: Bearer <API_KEY>
```

检查当前 Key 可见的模型：

```bash
curl "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $API_KEY"
```

模型名必须以 API 返回结果、模型广场或目标 Key 所属分组的实际配置为准。

## 2. 文本对话

### Chat Completions

```http
POST /v1/chat/completions
```

```bash
curl "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "YOUR_TEXT_MODEL",
    "messages": [
      {"role": "user", "content": "用三句话介绍这个 API"}
    ],
    "stream": false
  }'
```

常用字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `model` | 是 | 当前 Key 所属分组可调用的文本模型 |
| `messages` | 是 | 按 `role` 和 `content` 传入的对话消息 |
| `stream` | 否 | `true` 时使用 SSE 流式接收 |

## 3. 图片生成

OpenAI 兼容图片接口：

| 用途 | 方法 | 路径 |
| --- | --- | --- |
| 文生图 | `POST` | `/v1/images/generations` |
| 图片编辑或图生图 | `POST` | `/v1/images/edits` |

API Key 必须绑定已开放图片生成能力且有可用图片模型的分组。

### 图片请求字段

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `model` | 是 | 图片模型名称，例如 `gpt-image-2` |
| `prompt` | 文生图必填 | 图片描述或编辑指令 |
| `response_format` | 否 | `b64_json` 或 `url` |
| `n` | 否 | 生成数量，默认 `1`，实际范围由模型决定 |
| `size` | 否 | 图片尺寸，支持范围由模型决定 |
| `stream` | 否 | `true` 时使用 SSE 接收图片事件 |

### response_format 优先级

1. 请求体显式传入 `response_format` 时，以请求参数为准。
2. 请求未传入时，使用 API Key 所属分组的 `image_response_format`。
3. 分组未配置时，回退为 `b64_json`。

### URL 方式

请求：

```bash
curl "$BASE_URL/v1/images/generations" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "一座雨夜中的未来城市，电影感光线",
    "size": "1536x1024",
    "n": 1,
    "response_format": "url"
  }'
```

响应：

```json
{
  "created": 1710000000,
  "data": [
    {
      "url": "https://api.example.com/generated-images/example.png"
    }
  ]
}
```

读取 `data[].url`。该地址无需 API Key 即可访问，默认 24 小时后失效。业务需要长期保存时，应立即下载到自己的对象存储或文件系统。

### Base64 方式

请求：

```bash
curl "$BASE_URL/v1/images/generations" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "白色背景上的产品摄影，柔和棚拍光",
    "n": 1,
    "response_format": "b64_json"
  }'
```

响应：

```json
{
  "created": 1710000000,
  "data": [
    {
      "b64_json": "iVBORw0KGgoAAAANSUhEUgAA..."
    }
  ]
}
```

读取 `data[].b64_json`。字符串不包含 `data:image/...;base64,` 前缀，客户端必须 Base64 解码后保存，或根据真实图片格式自行拼接 Data URI。

Python 解码示例：

```python
import base64
import os
import requests

response = requests.post(
    f"{os.environ['BASE_URL']}/v1/images/generations",
    headers={"Authorization": f"Bearer {os.environ['API_KEY']}"},
    json={
        "model": "gpt-image-2",
        "prompt": "极简风格的玻璃水杯产品图",
        "response_format": "b64_json",
    },
    timeout=300,
)
response.raise_for_status()

image_base64 = response.json()["data"][0]["b64_json"]
with open("generated-image.png", "wb") as image_file:
    image_file.write(base64.b64decode(image_base64))
```

### 上传本地图片进行编辑

本地文件使用 `multipart/form-data`，不要手动设置 multipart boundary。

```bash
curl "$BASE_URL/v1/images/edits" \
  -H "Authorization: Bearer $API_KEY" \
  -F "model=gpt-image-2" \
  -F "prompt=保留主体，把背景改成日落海边" \
  -F "image=@./source.png" \
  -F "n=1" \
  -F "response_format=url"
```

### 使用公网 URL 作为参考图

```bash
curl "$BASE_URL/v1/images/edits" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "将参考图改成水彩插画",
    "images": [
      {"image_url": "https://example.com/source.png"}
    ],
    "response_format": "url"
  }'
```

远程地址必须是服务器可访问的公网 HTTP/HTTPS 图片。地址不可访问、返回非图片内容或超过限制时，接口返回 `400 invalid_request_error`。

### 流式图片

图片流式请求使用 SSE：

- 选择 `url` 时，中间 `partial_image` 事件仍可能携带 `b64_json`，最终完成事件返回 `url`。
- 选择 `b64_json` 时，最终完成事件返回 `b64_json`。

## 4. 视频生成

视频生成是异步任务：

1. `POST /v1/videos/generations` 创建任务。
2. `GET /v1/videos/{request_id}` 轮询状态。
3. `GET /v1/videos/{request_id}/content` 下载视频。

### 视频模型与图片输入

| 模型 | 图片输入 | 限制 |
| --- | --- | --- |
| `grok-imagine-video` | `reference_images[].url` | 最多 4 张参考图，用于人物、物品或风格参考，不固定为首帧 |
| `grok-imagine-video-1.5` | `image.url` | 最多 1 张起始图；未传起始图时按标准视频模型路由 |

不要在同一请求中同时传 `image` 和 `reference_images`。内联 Data URL 图片解码后不得超过 1 MB，否则返回 `413`。

### 标准模型参考图引导

```bash
curl "$BASE_URL/v1/videos/generations" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-video",
    "prompt": "围绕产品缓慢运镜，保持主体细节稳定",
    "resolution": "720p",
    "duration": 10,
    "reference_images": [
      {"url": "https://example.com/product.jpg"}
    ]
  }'
```

### 1.5 模型起始图生成

```bash
curl "$BASE_URL/v1/videos/generations" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-video-1.5",
    "prompt": "镜头向前推进，云层缓慢移动",
    "resolution": "1080p",
    "duration": 8,
    "image": {
      "url": "data:image/jpeg;base64,YOUR_COMPRESSED_IMAGE_BASE64"
    }
  }'
```

创建接口通常返回：

```json
{
  "request_id": "video-request-id"
}
```

### 轮询任务状态

建议每 2 秒查询一次，并由客户端设置最长等待时间。

```bash
export REQUEST_ID="video-request-id"

curl "$BASE_URL/v1/videos/$REQUEST_ID" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Accept: application/json"
```

完成状态通常为：

- `completed`
- `succeeded`
- `success`
- `done`

失败状态通常为：

- `failed`
- `error`
- `cancelled`
- `canceled`

### 下载视频

任务完成后，通过本站 content 接口携带同一个 API Key 获取视频：

```bash
curl "$BASE_URL/v1/videos/$REQUEST_ID/content" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Accept: video/mp4,video/*" \
  --output generated-video.mp4
```

不要依赖状态响应中的上游 CDN 地址。content 接口会使用任务绑定账号代理读取视频，并限制允许的来源、内容类型和文件大小。

## 5. 错误处理

| 状态码 | 常见原因 | 处理建议 |
| --- | --- | --- |
| `400` | 字段缺失、格式错误、模型与图片输入方式不匹配 | 核对请求体、`Content-Type` 和字段名 |
| `401` | API Key 缺失、无效或已被撤销 | 确认使用 `Authorization: Bearer <API_KEY>` |
| `403` | 分组未开放目标能力，或余额与权限检查未通过 | 检查 Key 绑定分组、余额和图片/视频能力 |
| `404` | 模型或入口不可用，或临时图片 URL 已过期 | 检查模型列表；URL 图片应在 24 小时内转存 |
| `409` | 视频任务尚未完成，content 暂不可读取 | 继续轮询，不要立即重复创建任务 |
| `413` | 视频内联参考图超过 1 MB 等请求体限制 | 压缩图片后重试 |
| `429` | 用户、Key、账号并发或上游频率受限 | 尊重 `Retry-After`，使用指数退避 |
| `5xx` | 上游异常、无可调度账号或网关暂时不可用 | 保留请求标识与响应体，稍后重试或联系管理员 |

标准错误响应通常包含：

```json
{
  "error": {
    "type": "invalid_request_error",
    "message": "Error details"
  }
}
```

## 6. 上线前检查

- API Key 只存在服务端，不会随网页代码或客户端安装包公开。
- 已用最小文本请求确认 Base URL、鉴权和目标模型可用。
- 图片客户端能同时解析 `data[].b64_json` 与 `data[].url`。
- URL 图片会在 24 小时内下载或转存。
- 视频创建、轮询、失败状态和 content 下载均设置超时。
- 对 `429` 和临时 `5xx` 使用有限次数的带抖动指数退避。
