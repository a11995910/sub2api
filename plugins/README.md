# 本地插件开发工作区

本目录用于存放 Sub2API 插件的开发源码和构建产物，属于 Sub2API 主仓库并
纳入版本控制，不得再通过根目录忽略规则整体排除。各插件的 `dist/` 输出也可
随对应源码一起提交，但签名私钥、运行配置和凭据仍禁止入库。

建议每个插件使用独立子目录。动态进程插件打包为 `.s2plugin`，通过
`backend/pkg/pluginapi/` 的公开协议运行，推荐结构如下：

```text
plugins/
├── <plugin-name>/
│   ├── cmd/
│   ├── internal/
│   ├── ui/
│   ├── tools/
│   ├── manifest.source.json
│   └── dist/
└── dist/                 # 可选的本地成品汇总目录
```

插件协议以 `backend/pkg/pluginapi/` 为准，开发要求和边界以
`docs/PLUGIN_DEVELOPMENT.md` 为准。涉及用户身份、用量账本或余额事务的功能若
无法通过公开能力安全实现，应作为宿主内置功能开发，不能让插件直接连接宿主数据库。
签名私钥、Token、代理凭据及其他敏感信息不得写入源码、构建包或 Sub2API 仓库；
`.env` 和常见私钥文件由根目录
`.gitignore` 保持忽略。
