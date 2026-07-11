# v0.1.151 上游整合说明

## 整合基线

- 上游来源：`upstream/main`
- 上游提交：`e316ebf52838a89d57fc790981cce7520f819ac8`
- 上游版本：`0.1.151`
- 本地同步分支：`sync/upstream-20260711`

本次保留上游合并提交，并将上游对已被本地集中实现取代的 5 个拆分文件改动迁移到当前文件：

- `openai_gateway_service.go`
- `openai_ws_forwarder.go`
- `setting_service.go`

## 功能变化

- OpenAI Fast/Flex 策略支持按 Sub2API 用户 ID 生效，用户专属规则优先于全局规则。
- Codex OAuth 请求根据最终 `User-Agent` 配对 `originator`，覆盖 HTTP、透传和 WebSocket 路径。
- Responses 与 Anthropic 转换补齐缓存创建 token。
- Responses → Chat fallback 支持 custom、`tool_search`、namespace/MCP 工具及 Responses Lite `additional_tools`。
- 修复 compact keepalive writer 释放后的访问问题。

## 本地审查修复

- Grok Responses 路径现在执行用户级 Fast/Flex 策略，并按策略处理后的最终 tier 计费。
- 原生 Responses 主路径补齐 `force_priority`，上游请求和本地计费使用同一最终 tier。
- Fast/Flex 用户 ID 在前端和后端同时校验正安全整数与重复项。
- 系统设置、认证来源默认值和 Fast/Flex 策略合并为一次仓储写入，避免部分保存。
- 取消 `opsCaptureWriter` 对象池，并在内层继续包装 writer 时保持引用有效，避免跨请求重绑和外层访问日志状态码变成 0。
- Chat fallback、原生 HTTP 和 WebSocket 对齐缓存创建 token 的 presence 与兼容别名优先级，嵌套显式零值不会被覆盖，负值不会进入计费。
- namespace 强制选择无法无损降级时明确拒绝，不再静默变成自动选择。
- Responses Lite `input` 混合字符串消息时仍逐项提取 `additional_tools`，避免合法工具声明整体丢失。

## 数据库与配置影响

本次没有新增 SQL migration、Ent schema、环境变量、Go/Node 依赖或 Docker 配置。用户级规则保存在现有 `openai_fast_policy_settings` JSON 中。

`v0.1.150` 及更早版本不认识 `user_ids`。如果新版本已保存带 `user_ids` 的规则，直接回滚旧镜像会把这些规则当成全局规则，可能导致所有用户被 block、filter 或 force_priority。发布流程会单独快照该设置并记录上一镜像的能力位；只有回滚目标不支持用户规则时，脚本才区分安全、明确不安全和检查失败三种状态，并在明确不安全时恢复发布前单行快照。兼容版本之间回滚会保留当前设置；任何切换失败都会恢复回滚即时快照和当前镜像。

## 回归范围

- OpenAI Responses、Chat、Messages、WebSocket、Grok 和 Chat fallback。
- custom、exec、`tool_search`、namespace/MCP、`additional_tools` 的流式与非流式转换。
- 管理端设置加载、用户 ID 输入校验、原子保存和旧 JSON 兼容。
- `/admin/accounts`、`/api/v1/admin/accounts`、账号测试、额度刷新、`/purchase` 和 `/models`。
