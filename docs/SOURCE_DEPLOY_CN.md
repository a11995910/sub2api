# Sub2API 源码同步与部署说明

本文档记录当前源码仓库、VPS 源码目录和从源码编译部署的固定流程，避免后续在其他电脑修改时漏掉前端嵌入构建。

## 源码仓库

- GitHub：`git@github.com:a11995910/sub2api.git`
- 主分支：`main`
- 本地开发完成后，提交并推送到 GitHub：

```bash
git status
git add .
git commit -m "说明本次修改"
git push
```

## VPS 源码目录

建议源码放在：

```bash
/opt/sub2api-src
```

首次拉取：

```bash
git clone git@github.com:a11995910/sub2api.git /opt/sub2api-src
```

如果服务器没有配置 GitHub SSH Key，也可以使用 HTTPS：

```bash
git clone https://github.com/a11995910/sub2api.git /opt/sub2api-src
```

后续更新：

```bash
cd /opt/sub2api-src
git pull --ff-only
```

## 编译要求

部署产物必须包含前端静态资源，因此后端编译必须使用 `embed` 标签。当前仓库已经提供统一目标：

编译机器需要先准备：

- Go：以 `backend/go.mod` 中声明的版本为准
- Node.js / pnpm：用于构建 `frontend`
- make：用于执行仓库根目录的构建目标

```bash
cd /opt/sub2api-src
pnpm --dir frontend install
make build-deploy
```

构建产物位置：

```bash
backend/bin/server
```

## 替换当前运行版本

当前容器挂载的可执行文件路径：

```bash
/opt/sub2api-deploy/custom/sub2api-pool-overview
```

建议每次替换前先备份：

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

## 验证

```bash
docker compose ps
curl -I https://fast.youkeduo.site/health
curl -I https://fast.youkeduo.site/purchase
curl -I https://fast.youkeduo.site/models
```

期望结果：

- 容器状态为 `healthy`
- `/health`、`/purchase`、`/models` 返回成功状态码
- 前端页面刷新不出现 404

## 回滚

如果新版本异常，使用最近一次备份恢复：

```bash
cp /opt/sub2api-deploy/custom/sub2api-pool-overview.bak-时间戳 \
  /opt/sub2api-deploy/custom/sub2api-pool-overview
chmod +x /opt/sub2api-deploy/custom/sub2api-pool-overview
cd /opt/sub2api-deploy
docker compose restart sub2api
```

## OAuth 凭证说明

源码仓库不内置第三方 OAuth `client_id` / `client_secret`。如需使用相关登录流，请在运行环境中通过 `.env` 或服务环境变量注入：

- `GEMINI_CLI_OAUTH_CLIENT_ID`
- `GEMINI_CLI_OAUTH_CLIENT_SECRET`
- `ANTIGRAVITY_OAUTH_CLIENT_ID`
- `ANTIGRAVITY_OAUTH_CLIENT_SECRET`
