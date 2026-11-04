# 全球模型能力榜

入口位于后台顶部「入群」快捷链接左侧，路径 `/model-leaderboard`。黑金、黑紫独立页面包含前三名、指标切换、搜索、厂商筛选、本站已收录筛选、配置详情和模型广场跳转。支持手机横向表格与减少动画设置，每次显示 25 条，可继续加载。

## 数据来源与口径

唯一主来源是 [Artificial Analysis](https://artificialanalysis.ai/leaderboards/models) 的[官方免费 API](https://artificialanalysis.ai/documentation)：`GET https://artificialanalysis.ai/api/v2/data/llms/models`，后端使用 `x-api-key` 请求头认证。页面保留来源署名及方法、API 文档链接。

主榜直接使用 `evaluations.artificial_analysis_intelligence_index`，不自行加权计算总分。模型以稳定 UUID 标识，名称、厂商、slug 沿用官方数据；Max、High、Fallback 等评测配置保留，分别排名。内置快照为 2026-11-04 实际读取的官方 API 响应，共 723 条模型配置，其中 96 条有智能指数，采集时间保存在 `seed.json`。

| 页面指标 | 官方字段 | 展示与排序 |
| --- | --- | --- |
| 智能指数 | artificial_analysis_intelligence_index | 原始指数，降序 |
| 编程 | terminalbench_v4_0 | Terminal-Bench 4.0，比例乘 100，降序 |
| 科学推理 | gpqa | GPQA，比例乘 100，降序 |
| 高难知识 | hle | Humanity’s Last Exam，比例乘 100，降序 |
| 长上下文 | lcr | AA-LCR，比例乘 100，降序 |
| 输出速度 | median_output_tokens_per_second | tokens/s，降序 |
| 响应延迟 | median_time_to_first_token_seconds | 首 token 秒数，升序 |

参考价格为 `pricing.price_1m_input_tokens` / `price_1m_output_tokens`，单位 USD / 百万 token，与本站定价不同。API 中编程、数学综合指数当前全部为 null，页面使用明确标注的具体评测项，不捏造这些综合指数。

null 不补零；速度和延迟的非正数视为未提供，评测零分及免费价格保留。同分并列，筛选保留名次；排序采用 API 原始精度。不同评测、不同智能指数版本不得直接比较。主榜未评出智能指数的配置不会进入主榜，但可进入其有数据的其他指标榜。

API 未提供统一的榜单更新时间或智能指数版本号，因此页面只标注「本站快照日期」和「本站最近同步」，不将模型发布日期冒充榜单更新日期，不承诺源站日更。

## 凭据与同步

凭据仅在后端运行时读取，优先 `ARTIFICIAL_ANALYSIS_API_KEY` 环境变量；未设置时读取 `pricing.data_dir/artificial-analysis-api-key` 文件。文件仅应允许应用运行用户读取，不纳入 Git、镜像、日志或前端。通过 Docker 环境变量配置时必须确认应用 compose 显式传入变量；只修改 compose 插值 `.env` 不保证变量进入容器。使用已有数据卷内凭据文件可避免修改容器环境映射。新配置凭据后重启应用以读取。

`GET /api/v1/model-leaderboard` 返回公开分数、来源、同步时间与状态，不返回请求头或密钥；沿用公开 IP 限流、可选 JWT 和后台模式权限。无需数据库迁移。

每 24 小时由首个访问触发后台同步，请求立即返回缓存，同一进程仅允许一个同步。无访问不请求上游。官方 API 额度为每天 1,000 次，正常同步仅每天约 1 次；故障后每小时最多由访问触发一次重试。上游请求限定固定 HTTPS 入口、35 秒上下文期限、4 MiB 响应，不跟随重定向，避免认证头泄露。

成功响应原子写入 `pricing.data_dir/artificial-analysis-leaderboard.json`，权限 `0600`；重启校验缓存后恢复。401、429、网络错误、非法数值、重复模型 ID、智能指数整体缺失时，保留旧数据与原同步时间，显示失败/过期状态。没有配置凭据时明确显示「自动同步尚未启用」，使用已核验官方快照。缓存写入失败仅当前进程保留更新。

页面仅在同步进行时每 5 秒查询状态，最多 12 次，离开即取消；不创建系统定时任务或任何部署自动化。

## 模型广场联动

分组来自原有 `GET /api/v1/model-plaza`，遵守启用开关、强制登录、专属分组与用户权限。接口失败、未登录和未收录分别提示，不从管理 API 获取分组。

匹配候选为官方 slug 与官方完整名称；只剥离明确的推理档位括号（如 `Max`、`High`、`Max, Default Fallback`），用于关联同一基础型号。统一分隔符与明确列出的供应商命名空间，日期和预览后缀保持不变，不使用前缀模糊匹配。页面提示基础型号收录不保证渠道支持同一评测配置。

跳转携带 `?model=<本站实际模型名>`，分组链接另带 `group=<ID>`。广场精确定位型号及分组；编辑搜索后恢复普通包含搜索。收录表示分组配置支持，不保证实时健康，也不表示用户已购买相应订阅。

## 验证与发布

```bash
cd backend
go test -race ./internal/service/modelleaderboard
# 使用已注入运行环境的 API Key，命令及日志中不出现密钥内容。
SUB2API_TEST_LIVE_LEADERBOARD=1 go test ./internal/service/modelleaderboard -run TestLiveSource -v
cd ../frontend
pnpm exec vue-tsc --noEmit
pnpm exec vitest run src/utils/__tests__/modelLeaderboard.spec.ts src/views/__tests__/ModelLeaderboardView.spec.ts src/components/modelPlaza/__tests__/ModelPlazaContent.deepLink.spec.ts src/i18n/__tests__/localeKeyCompleteness.spec.ts
```

正式上线遵循仓库既有 main、staging、用户确认、prod 流程；本功能开发不自动发布。
