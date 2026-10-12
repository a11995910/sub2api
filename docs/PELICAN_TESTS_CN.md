# 智能运维：鹈鹕测智

管理员通过「智能运维 → 鹈鹕测智」配置分组绘图测试。入口为 `/admin/intelligent-ops/pelican-tests`，兼容 `/admin/pelican-tests`。功能参考 [ranxi2001/sub2api](https://github.com/ranxi2001/sub2api) 的 `683bb0cee4f622f7862d9e7fe8f517f0ffce3a13`，移植分组测试、成本快照、作品展示与隔离预览实现，遵循仓库 LGPL-3.0 许可证。

## 配置与运行

每个计划绑定一个分组，配置模型、绘图题目、思考强度、每轮并行份数（1–8）以及标准五字段 Cron。默认题目为用 SVG 绘制鹈鹕骑自行车的二维动画；频率提供每 15/30 分钟、每小时、每 2/6 小时、每天 9 点和自定义选项，按服务器时区执行。计划支持编辑、启停、删除和立即运行。已建立的计划不能更换分组；立即运行不改变下次定时执行时间。

每份输出经过现有网关调度器选号并占用账号并发槽，槽满最多等待一分钟。组合分组先解析目标平台与上游模型，再选择账号。作答复用账号测试的上游协议、认证、模型映射与代理；OpenAI/兼容接口及 Claude 按各自协议传递思考强度，不支持该参数的平台保持原有行为。原有账号连接测试与账号轮候保持独立。

账号在产生内容前发生上游错误可换号，最多尝试三个账号；空输出、非 HTML/SVG、拒答和输出截断保留为失败结果。记录包含最终账号、之前尝试的账号、错误、模型与配置快照、耗时和已知成本，不自动打分或调整账号优先级。每份原始正文最多 2 MiB，SSE 捕获最多 4 MiB。

定时任务由服务端每分钟扫描，关闭网页后继续执行。单计划使用数据库租约防重，单轮最多执行十分钟，崩溃后的租约十五分钟失效。管理员记录保留最近七天、每计划最多一百条；列表分页读取摘要，查看时才读取正文，页面每十五秒刷新当前页。

## 成本与作品展示

测试消耗上游额度，但不扣用户余额。成本使用上游实际返回的 token 用量、实际返回模型的美元定价及本仓库账号成本倍率 `BillingRateMultiplier()`，不套用用户或分组售价。流式累计用量不重复累加，换号前和失败调用已返回的用量也计入。无用量或无定价显示「未记录」，部分调用不可计价显示「部分」。今日按服务器时区统计；历史清理或删除计划不减少分组累计成本。

「用户展示」控制 `/pelican-showcase` 及用户侧边栏入口。默认关闭；关闭时仍执行测试并收录成功作品。展示页仅公开分组、模型、思考强度、耗时及作品，不公开账号身份、题目和成本。仅展示有测试计划且有效的分组，暂停计划仍保留展示资格。删除同组最后一个计划后不再展示该组，其作品由后台清理。

每组可保留 1–100 张作品，默认二十张；可开启按 1–90 天清理，默认七天。管理员可以移除指定作品。生成的 HTML/SVG 通过 `sandbox="allow-scripts"` iframe 预览，使用独立 CSP 限制外部资源、连接、嵌套框架和表单，不给予同源权限。展示内容不应被直接插入主页面执行。

## 接口与权限

以下管理接口统一使用现有管理员鉴权、审计和面板限流：

- `GET/POST /api/v1/admin/pelican-group-tests`：列出或新建计划。
- `PUT/DELETE /api/v1/admin/pelican-group-tests/:id`：编辑或删除计划。
- `POST /api/v1/admin/pelican-group-tests/:id/run`：后台立即运行，重复执行返回 409。
- `GET /api/v1/admin/pelican-group-test-results`：按 `page`、`page_size` 分页，可通过 `plan_id` 筛选。
- `GET /api/v1/admin/pelican-group-test-results/:id`：读取含原始正文的记录。
- `GET/PUT /api/v1/admin/pelican-showcase/settings`：展示开关、API 开关及保留规则。
- `DELETE /api/v1/admin/pelican-showcase/items/:id`：移除作品。

登录用户可读取 `GET /api/v1/pelican-showcase` 和 `GET /api/v1/pelican-showcase/items/:id`。另提供 `GET/HEAD /api/v1/public/pelican-showcase` 及 `/items/:id`，通过有效的本站 API Key 读取；需要同时开启用户展示和 API 读取开关。该接口不触发模型调用、不扣余额、不写入 Key 最近使用时间，但仍校验 Key 有效期、用户、分组和 IP 权限。清单为六十秒共享快照，正文有界缓存，支持 ETag/304 与 gzip；客户端建议至少间隔六十秒轮询。HTTP 缓存为私有且需要重新验证，不允许共享代理绕过鉴权。

## 数据、验证与回滚

迁移 `246_pelican_group_tests.sql` 新增四张独立表：计划、结果、分组每日成本及作品快照；不变更现有连接测试表。展示设置写入 `settings` 的 `pelican_showcase_enabled`、`pelican_showcase_api_enabled` 和 `pelican_showcase_config`。旧版本不读取这些表，回滚镜像后停止执行新功能，新增数据仍保留。删除计划会级联删除该计划的管理员记录，成本累计保留，作品按展示规则清理。

本地单元测试涵盖参数、换号、租约、成本计价、接口鉴权、展示开关、分页和预览 CSP。仓储测试可使用项目已有 Docker 集成测试环境，也可使用专门创建的空临时 PostgreSQL 数据库：

```bash
cd backend
PELICAN_TEST_POSTGRES_DSN='指向本地临时空数据库的 DSN' \
  go test -tags=pelicanpostgres ./internal/repository -run TestPelican -count=1
```

该命令会在指定数据库执行全量迁移并写入测试数据，只能使用临时数据库。测试不创建生产计划，也不调用真实模型。发布仍按仓库 staging 验证和用户确认 prod 的流程执行。
