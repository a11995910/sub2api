# 生图接口说明

本文重点说明 OpenAI 图片网关的接口和分组默认回传类型。Grok 图片分组使用独立的媒体转发逻辑，其上游响应格式不在本文范围内。

面向 API 使用者的公开站内文档位于 `/developer-docs`，无需登录即可访问；供 AI 或纯文本抓取器直接读取的版本位于 `/developer-docs.md`。

## 1. 接口地址

Sub2API 提供 OpenAI 兼容的图片接口：

| 用途 | 方法 | 路径 |
| --- | --- | --- |
| 文生图 | `POST` | `/v1/images/generations` |
| 图片编辑 / 图生图 | `POST` | `/v1/images/edits` |

请求需要携带 API Key：

```http
Authorization: Bearer <API_KEY>
Content-Type: application/json
```

API Key 必须绑定已开启 `allow_image_generation` 的分组，否则返回 `403 permission_error`。

## 2. 基本请求

```bash
curl "$BASE_URL/v1/images/generations" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "一只戴着宇航员头盔的橘猫",
    "response_format": "url"
  }'
```

常用字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `model` | 是 | 图片模型名称 |
| `prompt` | 文生图必填 | 图片描述 |
| `response_format` | 否 | `b64_json` 或 `url`；不传时使用分组默认值 |
| `n` | 否 | 生成数量，默认 `1` |
| `size` | 否 | 图片尺寸，具体取值由上游模型支持范围决定 |
| `stream` | 否 | 是否使用流式响应 |

`/v1/images/edits` 还支持图片上传；JSON 请求可使用网关支持的 `images[].image_url` 传入公网图片地址。

## 3. 回传类型

### `b64_json`（非流式）

返回图片的 Base64 字符串，字段为 `data[].b64_json`。字符串不包含 `data:image/...;base64,` 前缀。

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

客户端需要根据图片格式自行拼接 Data URI，或先 Base64 解码后保存文件。

### `url`（非流式）

返回图片地址，字段为 `data[].url`。网关会先将图片保存到本地，再返回 `/generated-images/...` 地址。

```json
{
  "created": 1710000000,
  "data": [
    {
      "url": "https://api.example.com/generated-images/8f3c...png"
    }
  ]
}
```

URL 默认有效期为 24 小时，过期后返回 `404`。配置了系统 `api_base_url` 时返回绝对 URL，未配置时可能返回同域相对 URL。部署时需要保证 `IMAGE_STORAGE_PATH` 可写，并在多实例部署中共享该存储目录。

流式请求使用 SSE 返回事件：选择 `url` 时，中间 `partial_image` 事件可能仍包含 `b64_json`，最终完成事件返回 `url`；选择 `b64_json` 时，最终完成事件返回 `b64_json`。

## 4. 分组默认设置

管理端入口：`分组管理 -> 图片生成计费 -> 默认传输方式`。

对应字段如下：

```json
{
  "allow_image_generation": true,
  "image_response_format": "url"
}
```

`image_response_format` 只允许以下值：

| 配置值 | 返回字段 | 适用场景 |
| --- | --- | --- |
| `b64_json` | `data[].b64_json` | 客户端需要直接取得图片内容 |
| `url` | `data[].url` | 客户端通过地址下载或展示图片 |

响应格式优先级：

1. 请求体显式传入 `response_format`，以请求参数为准。
2. 请求未传入时，使用 API Key 所属分组的 `image_response_format`。
3. 分组配置为空或旧数据未配置时，回退为 `b64_json`。

管理端 API 创建或更新分组时，也可以直接传递 `image_response_format`：

```http
POST /api/v1/admin/groups
PUT /api/v1/admin/groups/:id
```

## 5. 常见错误

| 状态码 | 场景 |
| --- | --- |
| `400` | 缺少 `model`、请求格式错误或 `response_format` 不是 `b64_json` / `url` |
| `401` | API Key 无效或未携带 |
| `403` | API Key 所属分组未开启图片生成 |
| `404` | URL 图片已超过 24 小时有效期或文件不存在 |
