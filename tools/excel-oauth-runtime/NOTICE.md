# Excel 自动授权运行时来源

Excel PKCE 适配器、密码/TOTP 流程、对应离线测试及可迁移运行时打包方式源自：
https://github.com/ranxi2001/sub2api
固定提交：837c6589cb793152826af6d8890af6e7478ce7b1
保留原许可于 LICENSE.ranxi。本项目增加受限标准输入协议、任务取消清理、环境隔离、邮箱环境变量传递及强制 TLS 证书校验（包括本机代理）。

toSub2 协议依赖固定为：
https://github.com/poxiao33/toSub2
提交：8548397e89bf80e508eda64a87e0d556d43abc84
构建产物保留其 LICENSE、依赖锁和运行时清单。

不引入对方的代理池、出口轮换、Codex 自动重登、TOTP 更换或 Web Session 导出。
