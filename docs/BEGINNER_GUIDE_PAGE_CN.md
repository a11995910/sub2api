# 小白使用攻略页面说明

## 功能入口

小白使用攻略页面是面向首次使用用户的公开教程页面，访问路径为：

```text
/beginner-guide
```

首页默认内容中提供“新手必看”公告入口；用户登录后的顶部导航也提供“小白攻略”入口。页面不要求登录，后端模式下未登录用户也可访问该页面。

## 使用角色

- 未登录用户：可查看下载入口、整体流程、常见问题；点击“已登录，去新建密钥”会进入登录流程或密钥页面。
- 已登录普通用户：可从页面跳转到 `/keys` 创建 API 密钥，并在密钥列表中执行“使用密钥”或“导入到 CCS”。
- 管理员：同样可查看页面；管理员还可在系统设置中控制 API Base URL、是否隐藏 CCS 导入按钮等公共配置。

## 操作流程

页面顶部提供三个页签：

- `Codex`：默认页签，保留原有 Codex + CC-Switch 入门流程。
- `Claude Code`：面向 Claude Code 用户，说明安装 Claude Code、安装 CC-Switch、创建 Claude 分组密钥并导入 CCS。
- `生图`：说明网页图片模式和本地生图工作台两种使用方式。

### Codex 流程

1. 用户打开 `/beginner-guide`，默认进入 `Codex` 页签。
2. 用户先下载 Codex 和 CC-Switch。
3. Codex CLI 用户按官方 CLI 文档安装；Codex 桌面端用户按 Windows 或 macOS 入口下载。
4. CC-Switch 用户按系统选择 GitHub Release 中的安装包。
5. 用户登录平台后进入 `/keys`。
6. 用户创建 API 密钥，并确认密钥绑定了正确分组。
7. 用户点击密钥行中的“导入到 CCS”，系统通过 `ccswitch://v1/import` deeplink 将站点地址、密钥、客户端类型、默认模型配置和用量脚本交给 CC-Switch。OpenAI 分组导入为 Codex Provider，默认模型为 `gpt-5.5`，默认 `model_reasoning_effort` 为 `medium`。
8. 用户在 Codex 中执行简单测试，确认密钥、分组、模型和接口地址可用。

### Claude Code 流程

1. 用户切换到 `Claude Code` 页签。
2. 用户按 Claude Code 官方 setup 文档安装。页面展示原生安装脚本，并补充 npm 全局安装方式；npm 安装要求 Node.js 18 或更高版本。
3. 用户安装并打开 CC-Switch。
4. 用户进入 `/keys` 创建 API 密钥，并确认密钥绑定 Claude / Anthropic / Antigravity Claude 兼容分组。
5. 用户点击密钥行中的“导入到 CCS”。Anthropic 分组导入为 Claude Provider；Antigravity 分组按对应端点生成配置。
6. 用户在项目目录运行 `claude` 做简单测试，确认没有鉴权、分组或模型路由错误。

### 生图流程

1. 用户切换到 `生图` 页签。
2. 网页路线：用户进入 `/model-test`，切换到图片模式，选择支持图片生成的模型、分组和密钥后输入提示词测试。
3. 本地路线：用户下载本地生图工作台安装包。
4. 用户打开本地生图工作台设置，填写平台生图 Key 和接口地址。
5. 用户用简单提示词执行首次生成，确认分组、模型、尺寸和接口地址可用。

## 涉及页面与模块

- `/beginner-guide`：小白使用攻略页面。
- `/home`：默认首页展示“新手必看”公告入口。
- `AppHeader`：登录后顶部导航展示“小白攻略”入口。
- `/keys`：创建密钥、查看密钥、使用密钥、导入到 CCS。
- `/model-test`：生图页签引导用户使用图片模式进行网页生图测试。
- `UseKeyModal`：根据密钥分组展示 Codex CLI、Claude Code、Gemini CLI、OpenCode 等配置示例。
- `ccswitchImport` 工具：生成 CC-Switch deeplink，并为 OpenAI 分组写入 Codex 默认模型和推理强度配置。

## 涉及接口

攻略页面本身不新增后端接口。页面会读取既有公共设置：

- 公共设置加载：用于展示站点名称、Logo、API Base URL。
- `/keys` 相关接口：用户进入密钥页后用于创建、查询、更新和删除 API 密钥。
- `/v1/usage`：CC-Switch 导入配置中的用量查询脚本会使用该路径查询密钥状态和余额。

## 涉及数据

攻略页面不新增数据表，不写入数据库。关联数据来自既有模块：

- API 密钥数据：由 `/keys` 页面创建和管理。
- 用户分组数据：决定“使用密钥”和“导入到 CCS”时生成的客户端配置类型；OpenAI 分组导入 CCS 时使用 Codex 配置，默认模型为 `gpt-5.5`，默认推理强度为 `medium`。
- 图片生成分组数据：决定 `/model-test` 图片模式能否选择图片模型、是否可以执行图片生成请求。
- 公共设置：包括站点名称、Logo、API Base URL、是否隐藏 CCS 导入按钮。

## 边界与异常处理

- Codex 桌面端当前只提供 Windows 和 macOS 官方入口；Linux 用户在页面中引导使用 Codex CLI。
- 若 CC-Switch 未安装或协议处理程序未注册，点击“导入到 CCS”后浏览器可能无响应；密钥页会提示用户安装 CC-Switch 或手动复制配置。
- 若 CC-Switch 检查地址仍显示旧 IP、内网地址或直连端口，先确认后台“API 端点地址”已填写客户端本机可访问的 HTTPS 公网地址；修改后需要删除 CC-Switch 中旧 Provider，并从密钥页重新导入。
- 若管理员隐藏 CCS 导入按钮，用户可点击“使用密钥”，按弹窗中的配置文件或环境变量手动配置。
- 若 API 密钥未绑定分组，“使用密钥”弹窗会提示用户先分配分组，避免生成错误客户端配置。
- Claude Code 用户必须确认密钥所在分组支持 Claude / Anthropic / Antigravity Claude，不要把 OpenAI / Codex 分组误用于 Claude Code。
- 生图用户必须确认密钥所在分组开启图片生成能力；若 `/model-test` 图片模式没有可选模型，优先检查分组权限、模型广场和可用渠道。
- 本地生图工作台下载链接使用站点静态路径 `/downloads/image-studio/`。安装包体积较大，不应放入前端构建产物或 Git 仓库；生产部署时应由 Nginx/Caddy/对象存储等静态文件服务承载该路径。
- 密钥属于敏感信息，页面提示用户不要公开、截图和录屏时必须打码。

## 下载来源

- Codex CLI：OpenAI 官方 Codex CLI 文档。
- Codex 桌面端：OpenAI 官方 Codex App 文档和官方下载入口。
- Claude Code：Anthropic / Claude Code 官方 setup 文档。
- CC-Switch：`farion1231/cc-switch` GitHub Release，页面展示当前核实版本和最新版本入口。
- 本地生图工作台：由站点运营方提供 macOS 和 Windows 安装包，生产环境需放置在 `/downloads/image-studio/` 对应静态目录下。
