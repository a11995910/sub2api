# Excel Bridge 独立上游

内置账号协议的配置见 [内置 Excel / BPS 协议](EXCEL_BPS_CN.md)。两者为独立接入方式：内置 BPS 直接使用所选 OAuth 账号转发，不经过本文的 Bridge 容器或插件。

Excel Bridge 以独立容器接入正式 VPS 的 Sub2API，不通过 `.s2plugin` 插件管理页安装。客户端仍访问 Sub2API，鉴权、分组限制、计费和使用记录由 Sub2API 处理。

## 平台多账号改造

原版单会话 Bridge 不适合批量复用平台 OAuth 账号。独立改造仓库为 `https://github.com/a11995910/sub2api-excel-oauth-bridge`（私有），基于 Bridge v0.5.3，设计参考相关维护项目 `zhu961212/sub2api-oai-basispoints` v0.6.9；该维护项目尚不能确认是用户原附件的官方更新来源。新插件 ID 为 `com.sub2api.plugin.excel-oauth-bridge`，版本 0.1.0。

新方案通过 `.s2plugin` 接收宿主当前调度账号的身份与代理，交给独立内网服务；不复制账号池、不读取个人 Codex 登录态、不逐个登录 Excel，账号刷新、调度、使用记录及计费仍由宿主负责。支持明确全选现有及新增 OAuth 账号，也可按 ID 和模型限定测试范围。

源码位于 VPS `/opt/excel-oauth-bridge/repo`，固定代码提交 `590d9ebb6a1c46852a893bc27582b66f2b6605a6` 的镜像为 `excel-oauth-bridge:590d9ebb6a1c`。已通过 281 项 Python 测试、101 子测试、Go 竞态/vet/子进程协议测试，以及隔离 Docker 的 12 账号并发身份与代理验证。测试仅使用合成凭据，临时网络和容器已清理。

新插件尚未安装或绑定正式宿主，也未进行真实 OAuth 推理与端到端扣费验收。下方原版容器及停用测试账号仍是当前线上实际状态；不得把新镜像存在或隔离测试通过等同于已开放生产。后续安装、发布者公钥配置与恢复方法以独立仓库 `docs/PLATFORM_DEPLOY.md` 为准。

## 当前部署

- 主机：正式 VPS `205.185.113.15`，SSH 别名 `sub2api-new-vps`。
- 来源：`https://github.com/Kaixxrua/excel-codex-bridge.git`，版本 `v0.5.3`，完整提交 `168ed925548d6149a9dde15651b40211a926a3c9`。
- 源码：`/opt/excel-codex-bridge/repo`；运行配置：`/opt/excel-codex-bridge/runtime`。
- 镜像：`excel-sub2api:0.5.3-168ed925548d`；容器：`excel-sub2api`；Compose project：`excel-bridge`。保留旧镜像 `excel-sub2api:0.4.6-66c41df941fb` 用于回滚。
- 网络：`sub2api-prod_sub2api-network`；上游地址：`http://excel-sub2api:8000/v1`，无宿主机端口映射。
- 容器以 UID/GID `10001:10001` 运行，根文件系统只读，限制 1 CPU、1GiB 内存、128 个进程。
- 分组：`Excel Bridge 测试`（ID `100`），专属、停用，模型白名单限制为四个 `*-excel` 模型。
- 账号：`Excel Bridge 0.5.3（待同步会话）`（ID `11039`），OpenAI / API Key，开启 OpenAI 透传，停用且不可调度。

API key 与管理 key 分别存于运行配置目录的 `secrets/api-key`、`secrets/admin-key`，文件权限 `0600`，供非 root 容器读取；父目录为 root-only。不得打印密钥或提交到 Git。管理 key 不交给 Sub2API。实例只保存一份导入的 ChatGPT 会话，且仅在内存中保存。

## 定价与验证状态

测试分组保留四个模型：`gpt-5.6-sol-excel`、`gpt-5.6-terra-excel`、`gpt-5.6-luna-excel` 和 `gpt-6-astra-excel`。账号映射保持恒等，bridge 内部才转换为上游模型名。测试分组按对应基础模型的 Sub2API 默认定价建立独立定价，倍率为 1；这不是已确认的对外售卖价格。

v0.5.3 的 bridge 模型接口提供六个基础模型的普通版和 `-1m-excel` 版，共十二个别名；新基础模型为 `gpt-6-sol` 和 `gpt-6-luna`。普通版上下文配置为 272k，长上下文版为 918k。新增别名尚未加入 Sub2API 分组白名单，需完成独立定价、长上下文计费和真实调用验证后再开放，不能把后端模型列表等同于已对用户开放的模型。

当前镜像通过 270 项自动测试、101 项子测试，1 项跳过；隔离网络 Docker 冒烟测试通过，覆盖鉴权、管理面隔离、会话导入/清除和重启后丢失会话。测试使用合成凭据，不证明真实 OpenAI 调用可用。

尚未导入真实 ChatGPT 会话。实际推理、流式输出、工具调用、图片输入及实际扣费均待验证。健康接口成功只表示服务存活；无会话时推理返回 401 是预期行为。

## 同步 ChatGPT 会话

v0.5.3 支持两种会话来源：Codex 的 ChatGPT 登录态，或 Microsoft Excel 中 OpenAI ChatGPT 加载项的登录态；使用前者不需要安装 Excel。Codex 的 API Key 登录方式不适用。使用新版对应平台客户端，并显式选择来源，避免默认 `auto` 选中非预期账号；未经用户授权不要读取或同步其他应用的登录凭据。WPS 登录不等同于 Microsoft Excel 加载项会话。

在已完成所选登录、且已配置正式 VPS SSH 密钥与主机指纹的电脑执行一次同步。Windows v0.5.3 包的命令为（二选一）：

```powershell
.\excel-codex.exe sub2api push-session --ssh sub2api-new-vps --login codex
.\excel-codex.exe sub2api push-session --ssh sub2api-new-vps --login excel
```

`sub2api-new-vps` 必须是该电脑自身配置的 SSH 别名，不能假设另一台电脑已有相同配置。同步通过 SSH 标准输入将所选登录会话发送到正式 VPS，不复制登录缓存、私钥或 token 到仓库。此操作只同步已存在的会话，不负责登录、刷新订阅或延长会话。

会话导入后先验证专用账号，再开放测试分组，核对响应 `usage` 的输入、输出、缓存 token 与 Sub2API 使用记录和余额变动。bridge 在部分断流场景会补造完成事件，必须验证缺失用量时的处理，不能仅凭 HTTP 200 判断计费准确。

## 检查、重启与停用

```bash
ssh sub2api-new-vps
docker exec excel-sub2api excel-sub2api session-status
docker inspect excel-sub2api --format '{{.State.Health.Status}}'
```

状态命令仅输出是否已配置、是否过期及到期时间。不要读取或输出原始会话。容器重启后会话丢失，需再次同步；过期时需先通过所选来源重新登录或刷新，再同步。

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

版本回滚使用 `runtime/upgrade-backup-*` 中的 `compose.override.json` 恢复原镜像配置，再执行上述 `up -d --no-build` 与健康检查。升级备份路径及旧镜像记录在 `sub2api-integration.json`；凭据文件保持原样，回滚后仍需重新同步会话。

主应用的 `main`、staging、prod 发布规则继续适用；bridge 升级须另外核对固定源码版本、资源检查、隔离测试及会话重同步安排，不能使用其上游文档替代本项目发布授权。
