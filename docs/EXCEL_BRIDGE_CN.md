# Excel Bridge 独立上游

Excel Bridge 以独立容器接入正式 VPS 的 Sub2API，不通过 `.s2plugin` 插件管理页安装。客户端仍访问 Sub2API，鉴权、分组限制、计费和使用记录由 Sub2API 处理。

## 当前部署

- 主机：正式 VPS `205.185.113.15`，SSH 别名 `sub2api-new-vps`。
- 来源：`https://github.com/Kaixxrua/excel-codex-bridge.git`，版本 `v0.4.6`，完整提交 `66c41df941fb1a963801964c75ff24b4a19e93f2`。
- 源码：`/opt/excel-codex-bridge/repo`；运行配置：`/opt/excel-codex-bridge/runtime`。
- 镜像：`excel-sub2api:0.4.6-66c41df941fb`；容器：`excel-sub2api`；Compose project：`excel-bridge`。
- 网络：`sub2api-prod_sub2api-network`；上游地址：`http://excel-sub2api:8000/v1`，无宿主机端口映射。
- 容器以 UID/GID `10001:10001` 运行，根文件系统只读，限制 1 CPU、1GiB 内存、128 个进程。
- 分组：`Excel Bridge 测试`（ID `100`），专属、停用，模型白名单限制为四个 `*-excel` 模型。
- 账号：`Excel Bridge 0.4.6（待同步会话）`（ID `11039`），OpenAI / API Key，开启 OpenAI 透传，停用且不可调度。

API key 与管理 key 分别存于运行配置目录的 `secrets/api-key`、`secrets/admin-key`，文件权限 `0600`，供非 root 容器读取；父目录为 root-only。不得打印密钥或提交到 Git。管理 key 不交给 Sub2API。实例只保存一份 Excel 会话，且仅在内存中保存。

## 定价与验证状态

四个模型为 `gpt-5.6-sol-excel`、`gpt-5.6-terra-excel`、`gpt-5.6-luna-excel` 和 `gpt-6-astra-excel`。账号映射保持恒等，bridge 内部才转换为上游模型名。测试分组按对应基础模型的 Sub2API 默认定价建立独立定价，倍率为 1；这不是已确认的对外售卖价格。`gpt-6-astra` 在 bridge 源码中标注为尚未确认 Excel 后端支持，必须单独实测。

当前镜像通过 239 项自动测试、67 项子测试，1 项跳过；隔离网络 Docker 冒烟测试通过，覆盖鉴权、管理面隔离、会话导入/清除和重启后丢失会话。测试使用合成凭据，不证明真实 OpenAI 调用可用。

尚未导入真实 Excel 会话。实际推理、流式输出、工具调用、图片输入及实际扣费均待验证。健康接口成功只表示服务存活；无会话时推理返回 401 是预期行为。

## 同步 Excel 会话

先在用户自己的 Microsoft Excel 中安装 OpenAI 发布的 ChatGPT 加载项并登录。Windows 可使用用户已有的 `excel-codex-bridge-0.4.6-windows-x64` 包；macOS 应使用对应平台版本。WPS 登录不等同于 Microsoft Excel 加载项会话。

在已登录 Excel、且已配置正式 VPS SSH 密钥与主机指纹的电脑执行一次同步。Windows 包的命令为：

```powershell
.\excel-codex.exe sub2api push-session --ssh sub2api-new-vps
```

`sub2api-new-vps` 必须是该电脑自身配置的 SSH 别名，不能假设另一台电脑已有相同配置。同步通过 SSH 标准输入传递会话，不复制登录缓存、私钥或 token 到仓库。此操作只同步已存在的会话，不负责登录、刷新订阅或延长会话。

会话导入后先验证专用账号，再开放测试分组，核对响应 `usage` 的输入、输出、缓存 token 与 Sub2API 使用记录和余额变动。bridge 在部分断流场景会补造完成事件，必须验证缺失用量时的处理，不能仅凭 HTTP 200 判断计费准确。

## 检查、重启与停用

```bash
ssh sub2api-new-vps
docker exec excel-sub2api excel-sub2api session-status
docker inspect excel-sub2api --format '{{.State.Health.Status}}'
```

状态命令仅输出是否已配置、是否过期及到期时间。不要读取或输出原始会话。容器重启后会话丢失，需再次同步；过期时需先在 Excel 中登录或刷新，再同步。

复用当前镜像恢复容器：

```bash
docker compose --project-name excel-bridge \
  --env-file /opt/excel-codex-bridge/runtime/.env \
  -f /opt/excel-codex-bridge/repo/packaging/sub2api/compose.yaml \
  -f /opt/excel-codex-bridge/runtime/compose.override.json \
  up -d --no-build
bash /opt/sub2api/repo/deploy/release-gates wait-container-healthy excel-sub2api 120 3
docker exec excel-sub2api excel-sub2api session-status
```

回退时先在 Sub2API 停用该专用账号和分组，再执行 `docker stop excel-sub2api`；保留运行配置与密钥即可恢复。无需恢复 Sub2API 主应用镜像或数据库，也不要删除使用记录。新增账号和分组 ID 同时记录在服务器运行目录的 `sub2api-integration.json` 中。

主应用的 `main`、staging、prod 发布规则继续适用；bridge 升级须另外核对固定源码版本、资源检查、隔离测试及会话重同步安排，不能使用其上游文档替代本项目发布授权。
