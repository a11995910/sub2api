# AGENTS.md

本文件是本项目的协作与上线约束。所有 AI 助手、自动化脚本和人工协作在本仓库内工作时都必须遵守。

## 语言与沟通

- 所有回复、分析、计划、注释、文档和提交说明必须使用中文。
- 输出先给结论，再给依据；存在不确定信息时必须明确说明，并给出核实方式。
- 复杂问题必须先拆解任务，再逐步执行，不能跳过上下文直接修改。

## 修改代码前的要求

- 必须先阅读相关文件、调用链、类型定义、配置项、上下游逻辑和已有实现。
- 严禁猜测接口格式、字段含义、数据结构、配置语义和业务规则。
- 修复问题必须先定位根因，再实施修改，不能只修复表面现象。
- 修改后必须检查影响范围，包括页面、接口、数据表、权限、状态流转、兼容逻辑和异常场景。
- 不允许回滚或覆盖用户已有改动，除非用户明确要求。

## Git 与提交

- 修改前后都要查看 `git status --short`，确认工作区状态。
- 提交前必须检查 diff，确保只包含本次任务相关内容。
- 提交说明使用中文，格式保持简洁，例如 `docs: 规范源码定制上线流程`。
- `AGENTS.md` 被 `.gitignore` 忽略，首次加入仓库时必须使用 `git add -f AGENTS.md`。

## VPS 与生产操作

- 生产操作前必须确认当前运行来源，包括源码目录、Git remote、当前 commit、容器挂载路径和运行中的二进制路径。
- 线上敏感信息不得写入仓库，包括服务器密码、Token、数据库密码、OAuth 密钥和 Cookie。
- 涉及数据库写入、迁移或批量数据修复时，必须先确认表结构、影响范围、备份方式和回滚方式。
- 不确定线上实际状态时，优先通过只读命令核实，例如 `git status --short`、`git remote -v`、`git rev-parse HEAD`、`docker compose ps`、`docker compose logs --tail=200`。

## Sub2API 定制二进制上线规范

当前线上 `sub2api` 使用自定义二进制挂载运行，挂载文件为：

```bash
/opt/sub2api-deploy/custom/sub2api-pool-overview
```

正确源码目录为：

```bash
/opt/sub2api-src
```

上线必须从 `/opt/sub2api-src` 同源 Git 仓库完整构建：

```bash
cd /opt/sub2api-src
git status --short
git pull --ff-only
git rev-parse HEAD
pnpm --dir frontend install --frozen-lockfile
make build-deploy
./backend/bin/server --version
```

严禁使用以下方式覆盖线上：

- 从本地电脑打包 `backend` 目录上传后临时编译。
- 只执行 `go build -tags embed`，但没有重新构建同一次源码对应的前端资源。
- 从未知源码目录或工作区有未确认改动的目录编译线上二进制。
- 不备份当前二进制就直接替换挂载文件。

替换必须先备份：

```bash
ts=$(date +%Y%m%d-%H%M%S)
cp /opt/sub2api-deploy/custom/sub2api-pool-overview \
  /opt/sub2api-deploy/custom/sub2api-pool-overview.bak-$ts
cp /opt/sub2api-src/backend/bin/server \
  /opt/sub2api-deploy/custom/sub2api-pool-overview
chmod +x /opt/sub2api-deploy/custom/sub2api-pool-overview
cd /opt/sub2api-deploy
docker compose restart sub2api
```

上线后必须验证：

```bash
cd /opt/sub2api-deploy
docker compose ps
curl -I https://fast.youkeduo.site/health
curl -I https://fast.youkeduo.site/purchase
curl -I https://fast.youkeduo.site/models
docker compose logs --tail=200 sub2api
```

还必须人工或接口回归管理端账号页：

- `/admin/accounts` 页面能正常打开。
- `/api/v1/admin/accounts` 不返回 5xx。
- 账号列表字段显示正常，特别是模型能力、状态、可调度状态和错误信息。

更完整的部署说明见 `docs/SOURCE_DEPLOY_CN.md`。

## 文档同步

- 涉及 API、部署流程、运行方式、配置项、数据库结构、业务流程或异常处理策略变化时，必须同步更新 `docs` 或 README 中的对应说明。
- 文档要描述当前系统实际状态，不能写成临时变更记录。
- 如果代码、线上状态和旧文档不一致，先核实真实逻辑，再更新文档。
