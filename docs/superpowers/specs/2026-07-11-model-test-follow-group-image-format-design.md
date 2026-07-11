# 模型测试台跟随分组图片格式设计

## 目标

模型测试台发起图片生成或图片编辑请求时不显式传递 `response_format`，由网关按照 API Key 所属分组的 `image_response_format` 决定返回 `b64_json` 或 `url`。

## 方案

- 删除 `testImageGeneration` JSON 请求中的固定 `response_format: 'b64_json'`。
- 删除 `testImageEdit` FormData 中固定写入的 `response_format`。
- 保留 `ModelTestView` 已有的双格式解析：优先展示 `b64_json`，不存在时展示 `url`。
- 不新增 UI 选项，避免测试台参数覆盖分组默认配置。

## 兼容与验证

- 默认 Base64 分组行为不变。
- URL 分组返回的图片由现有 `<img src>` 逻辑展示。
- 请求层测试必须断言生成和编辑请求均不包含 `response_format`。
- 运行相关 Vitest、前端类型检查、Lint 和生产构建。

