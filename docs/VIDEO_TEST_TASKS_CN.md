# 模型测试台视频任务记录

## 适用范围

本功能只服务于站内“模型测试台”的视频测试。普通开发者直接调用 `/v1/videos`、`/videos` 或对应状态接口时，不会创建站内任务记录，OpenAI 兼容响应格式也不变。

模型测试台在创建请求中添加内部请求头：

```http
X-Sub2API-Model-Test: video
```

该请求头只用于站内流量识别，不属于公开 OpenAI 视频协议，不应写入第三方接入示例。

## 创建与持久化

视频创建仍进入真实 `/v1/videos` 网关，继续执行 API Key 鉴权、分组路由、账号调度、故障切换、用量记录和计费。上游返回任务 ID 后，网关在响应测试台之前保存：

- 当前用户、API Key ID 和分组 ID；
- 最终调度的上游账号 ID；
- 上游任务 ID、模型、提示词、分辨率、时长和参考图数量；
- 规范化状态、进度和最后一次上游响应。

数据库不保存 API Key 明文或参考图 Base64。Redis 任务绑定过期后，网关可从持久任务恢复账号并重建缓存。

## 用户接口

以下接口位于 `/api/v1`，使用登录态 JWT，不接受客户端指定用户或上游账号：

```text
GET    /api/v1/model-test/video-tasks?page=1&page_size=20
POST   /api/v1/model-test/video-tasks/:id/refresh
GET    /api/v1/model-test/video-tasks/:id/content
DELETE /api/v1/model-test/video-tasks/:id
```

- 列表只返回当前用户记录，按创建时间倒序。
- 刷新接口五秒内重复调用时返回缓存记录；查询成功后更新数据库。
- 内容接口只允许已完成任务，继续透传 Range 相关请求和响应头。
- 删除只移除站内记录，不取消已经提交给上游的任务。

## 状态语义

站内状态只有 `queued`、`in_progress`、`completed` 和 `failed`。

`queued` 与 `in_progress` 没有总等待超时。网络超时、连接错误、上游 5xx、限流或账号暂时不可用只写入 `last_poll_error`，任务继续保持原生成状态。只有上游明确返回失败或取消终态时才写入 `failed`。

页面可见时，测试台每五秒刷新未完成任务；页面隐藏或关闭时停止前端轮询，重新进入后立即补查。完成内容按需从上游代理，不转存到本地或对象存储。

## 保留与清理

`queued` 和 `in_progress` 不自动过期。`completed` 和 `failed` 在终态时间超过 30 天后由每小时清理任务删除。应用版本回滚不会删除 `model_test_video_tasks` 表，旧版本会直接忽略该表。
