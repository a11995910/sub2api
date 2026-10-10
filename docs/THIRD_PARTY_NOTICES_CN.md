# 第三方声明

## ZyphrZero/chatgpt2api

本项目图片尺寸工具中的部分前端逻辑基于 `ZyphrZero/chatgpt2api` 适配，包括：

- 图片尺寸、比例、分辨率归一化规则

来源仓库：

```text
https://github.com/ZyphrZero/chatgpt2api
```

许可证：

```text
MIT License

Copyright (c) 2026 kunkun

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Excel / BPS 适配器

Excel/BPS 协议包与相关测试移植自 [ranxi2001/sub2api](https://github.com/ranxi2001/sub2api)，固定提交为 `e8fe22e8b637ea7b26f11b081b39aa518d0a90ec`。来源仓库根许可证为 LGPL-3.0，与本仓库根许可证保持一致；协议包保留其原有作者和参考实现说明，见 `backend/internal/service/basispoints/NOTICE.md`。

本仓库对账号代理、调度、配置接口、计费及前端进行了适配，具体边界见 [内置 Excel / BPS 协议](EXCEL_BPS_CN.md)。结构化输出校验新增依赖 `github.com/santhosh-tekuri/jsonschema/v6` v6.0.3，其许可证为 Apache-2.0，原文随 Go 模块提供。
