# 灵石充值、订阅兑换与模型广场说明

## 功能范围

用户侧核心入口：

- `灵石充值`：用户在 `/purchase` 页面内嵌外部小铺购买余额兑换码，购买后前往 `/redeem` 兑换灵石余额。
- `我的订阅`：用户在 `/subscriptions` 页面查看订阅用量，并直接使用灵石兑换可售订阅套餐。
- `模型广场`：用户在 `/models` 查看当前可调用模型、可用分组，以及按用户有效倍率计算后的灵石价格。

用户侧菜单已不再展示“可用渠道”和“渠道状态”入口，相关路由保留，避免影响已有接口、调度逻辑和管理员能力。

## 充值页

充值页文件：`frontend/src/views/user/RechargeRedeemView.vue`

外部小铺地址固定为：

```text
https://pay.ldxp.cn/shop/R5D7AG9X
```

页面提供：

- 内嵌 iframe 购买入口。
- 新窗口打开小铺入口。
- 当前灵石余额展示。
- 跳转兑换页 `/redeem` 的按钮。

充值页只展示灵石充值相关内容，不再承载订阅购买入口。订阅套餐统一在“我的订阅”页面用灵石兑换。

后端 CSP 已在 `backend/internal/server/middleware/security_headers.go` 中把 `https://pay.ldxp.cn` 加入 `frame-src`。如果目标站点自身后续增加禁止嵌入的响应头或 CSP，iframe 仍可能空白；此时用户可使用“新窗口打开”购买兑换码，再回到兑换页使用。

## 订阅兑换

订阅页面文件：`frontend/src/views/user/SubscriptionsView.vue`

后端接口：

```text
POST /api/v1/payment/plans/:id/purchase-with-balance
```

兑换流程：

- 前端读取 `/api/v1/payment/plans` 展示所有可售订阅套餐，保留套餐原价格、划线原价、描述、有效期、特性和关联分组信息。
- 用户点击“用灵石兑换”后会先看到确认弹窗；用户确认后，前端才调用兑换接口。
- 后端校验套餐仍可售、分组仍为启用的订阅分组、用户状态正常且灵石余额充足。
- 后端在同一数据库事务中扣减用户灵石、生成一张 `subscription` 类型兑换码、标记兑换码已使用，并创建或续期对应用户订阅。
- 兑换码面额记录为本次套餐价格，备注会写入套餐名称和套餐 ID，管理员后续可以在兑换码记录中查看这笔订阅兑换。
- 用户余额不足时不会创建兑换码，也不会修改订阅。

## 模型广场

模型广场文件：`frontend/src/views/user/ModelMarketView.vue`

页面复用现有用户接口，不新增后端接口：

- `/api/v1/channels/available`：读取用户可访问分组与支持模型定价。
- `/api/v1/groups/rates`：读取用户专属分组倍率。

价格展示规则：

- 基础定价来源于渠道模型定价配置。
- 文本、Token、按次价格使用用户有效文本倍率：优先用户专属倍率，其次分组默认倍率。
- 图片价格在分组启用独立图片倍率时使用图片倍率，否则使用文本倍率。
- 图片生成能力作为独立模型展示：OpenAI 图片分组会生成 `image-2` 条目，普通 GPT 模型只展示非图片分组，避免把 `ChatGPT2API 图片` 这类图片分组挂到所有 token 模型下。
- 所有金额展示为灵石，不再使用 `¥` 或 `$` 符号。

模型广场只展示模型、平台、分组和价格，不展示渠道名称、渠道状态或上游账号状态，避免把调度细节暴露给普通用户。

## 货币单位

前端统一通过 `formatSpiritStones()` 或 `common.currencyName` 展示余额、用量、兑换码面额和后台金额字段。历史字段名如 `*_usd` 暂不变更，原因是它们已经参与接口、数据库字段、统计逻辑和旧数据兼容；本次仅调整用户可见单位文案。

## 影响范围

- 用户侧：菜单、充值页、兑换入口、模型广场、余额和用量展示。
- 管理侧：用户余额、分组额度、订阅额度、兑换码、推广码、订单、统计图表等金额展示文案。
- 后端：仅调整 CSP 白名单，不改变支付、兑换、扣费、渠道调度和数据库结构。

## 验证建议

- 前端类型检查：`./node_modules/.bin/vue-tsc --noEmit`
- 前端构建：`./node_modules/.bin/vite build`
- 源码部署编译：`make build-deploy`
- 后端测试：`go test ./...`
- 部署后检查 `/purchase`、`/redeem`、`/subscriptions`、`/models` 和用户侧菜单显示。
- 使用浏览器开发者工具确认响应头 `Content-Security-Policy` 的 `frame-src` 包含 `https://pay.ldxp.cn`。
