# DeepSeek Search / Fetch

DeepSeek 分组支持独立 `POST /v1/tools/search`、`POST /v1/tools/fetch`，并为原生 Anthropic 或 adaptive 协议的 `/v1/messages` 实现服务端 Fetch。工具请求转发至调度账号的工具上游，不在本站另建爬虫。

## 配置与计费

后台创建、编辑 DeepSeek 分组时填写“DeepSeek Search / Fetch 定价”的每千次成功调用售价，复用 `search_price_per_1k`。**留空拒绝新增工具入口，0 表示明确免费**；代码不把 DeepSeek 官方价格或 Grok 默认价套作本站售价。

- 独立 Search / Fetch：HTTP 200 且包含有效结果时计一次；空结果、失败、超时不计工具费。
- Messages 服务端 Fetch：按实际成功 Fetch 次数收费；每轮模型 Token 按既有定价分别结算，最后合并到本次用量记录。不同轮次的输入不会相加后误触发长上下文阶梯；模型按次定价则按实际模型轮数计费。
- 工具费 = 成功次数 × 每千次售价 ÷ 1000 × 基础有效倍率。用户专属倍率仍优先，不叠加高峰倍率；模型 Token 仍按原有规则处理高峰。
- 用量日志的 `billing_tier=deepseek_tools:N` 保存成功工具次数。独立工具的展示模型为 `deepseek-tools-search` 或 `deepseek-tools-fetch`，仅用于识别，不产生额外模型费用。
- 已有原生 Messages `web_search` 仍由供应商执行并沿用原路径计费，**不属于此次新增的独立 Search 调用费**。
- 多轮账号成本统计同样逐轮计算；工具先匹配 `deepseek-tools-fetch` / `deepseek-tools-search` 的显式账号统计规则，没有规则则沿用售价乘账号倍率的估算，不宣称这是第三方实际账单。模型缺价仍按既有策略告警并不收模型费用，但已明确的成功工具费用不会被清零。
- 下游断开前已经确认成功的上游调用仍记账；尚未确认结果的超时不猜测成功次数。工具传输失败不自动重放，避免重复调用。

工具入口沿用本站 Key 鉴权、分组隔离、用户与账号并发、订阅/余额/Key 额度检查，以及现有在途预留机制。Messages 的预留按最大工具次数和模型轮数保守估算，实际扣费以真实成功工具次数与每轮用量为准。

## 上游地址

工具使用已选 DeepSeek API Key 账号的 Chat Completions 基址拼接 `/v1/tools/search` 或 `/v1/tools/fetch`。adaptive 账号应显式配置 `api_base_urls.chat_completions`；若只配置了其他协议的第三方域名，不会把同一凭据静默发送到官方默认地址。

上游必须实际提供工具端点；仅支持模型对话不代表支持 Search/Fetch。账号代理沿用账号设置；本站客户 Key 不转发给目标网页。上游 HTTP 重定向被禁用，避免认证信息被重定向至其他域名。

## 独立工具请求

```http
POST /v1/tools/search
Authorization: Bearer <本站 DeepSeek 分组 Key>
Content-Type: application/json

{"query":"DeepSeek context caching","count":2}
```

`query` 为 1—128 个字符；`count` 为 1—50。可选参数沿用上游协议：`need_content`、`ori_question`、`localization`。结果按上游 JSON 返回。

```http
POST /v1/tools/fetch
Authorization: Bearer <本站 DeepSeek 分组 Key>
Content-Type: application/json

{"url":"https://www.deepseek.com/"}
```

Fetch 返回 `title` 和 `content`。仅接受公开 HTTP/HTTPS 地址；拒绝内网、回环、保留地址及携带用户名密码或片段的网址。请求体最多 64 KiB，工具响应最多 2 MiB，单次上游请求最长 30 秒。

## Messages 服务端 Fetch

```json
{
  "model": "deepseek-v4.1-flash",
  "max_tokens": 1024,
  "stream": true,
  "tools": [{"type": "web_fetch_20250910", "name": "web_fetch", "max_uses": 2}],
  "messages": [{"role": "user", "content": "抓取 https://www.deepseek.com/ 并总结页面。"}]
}
```

兼容 `web_fetch_20250910`、`web_fetch_20250305` 和 `web_fetch`，支持非流式、流式、工具结果回放和其他工具混用。模型普通工具调用由本站执行真实 Fetch，向客户端返回 `server_tool_use` / `web_fetch_tool_result`，再将抓取内容回填模型。

`max_uses` 可设 0—10，未提供时最大 10 次；最多执行 `max_uses+2` 轮模型调用，之后返回 `pause_turn`。每轮模型调用最多 5 分钟，总 `max_tokens` 跨轮递减；输出预算用尽返回 `max_tokens`，剩余预算不足以维持指定思考预算时返回 `pause_turn`。原生 Search 显式配置的 `max_uses` 也跨模型轮扣除，不因 Fetch 回填而重新获得次数。

DeepSeek 上游的思考模式不支持强制 `tool_choice=tool/any`。客户未指定 `thinking` 时，强制工具请求会显式使用 `thinking.type=disabled`；客户已明确要求思考时，提前返回 400 并提示改用自动工具选择或显式关闭思考。`auto/none` 不改写思考设置；完成第一轮强制工具调用后改为自动选择，允许模型生成最终回答。

单次回填客户端和模型的正文最多 128 KiB，总对话体最多 8 MiB。支持 `allowed_domains` 或 `blocked_domains`，两者不能同时非空；空数组视为不限制。域名统一按 IDNA 规范化，精确匹配主机或其子域。`max_content_tokens` 在没有供应商分词器时按 UTF-8 字节保守裁剪，不能视为精确分词统计。`citations.enabled=true` 明确返回 400，避免伪造引用；`citations.enabled=false` 或 `citations=null` 可用。`url_sources` 等未实现的工具参数明确拒绝，不声称与 Anthropic 原生 Fetch 的全部扩展能力等价。目标站点拒绝访问、动态网页没有正文、超过限额等情况会返回工具错误，不保证任意网址都可抓取。

本地 URL 校验不能替代上游抓取器对重定向和 DNS 重绑定的安全限制；应选择具备对应防护的工具供应商。

## 验收

本地行为测试覆盖真实 SSE 增量刷出、跨轮索引和用量汇总、工具混用、历史回放、次数和输出预算、取消/中断、价格缺失、逐轮阶梯、凭据隔离与 URL 限制。运行：

```sh
go test -tags=unit ./internal/service ./internal/handler ./internal/server/routes -run 'Test(DeepSeek|ValidateDeepSeek|ExecuteDeepSeek|NativeAnthropic|.*WebSearch|.*Billing|.*Inflight|.*AccountStats)' -count=1
```

可选真实上游测试由 `deepseek_live` 构建标签隔离。仅在显式设置 `SUB2_DEEPSEEK_LIVE=1` 时运行；账号配置由进程环境 `SUB2_DEEPSEEK_LIVE_CREDENTIALS` 和 `SUB2_DEEPSEEK_LIVE_EXTRA` 临时注入，不写入测试文件。该验收直接调用上游并产生上游用量，不写入本站业务数据库。
