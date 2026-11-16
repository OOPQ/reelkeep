# 相关标准与取舍

## 有没有现成的「视频源规则」标准？

**没有可以直接套用的。** 最接近的两个生态都不是这个形状：

| 生态 | 实际形态 | 能不能直接用 |
|---|---|---|
| Legado / 阅读（书源） | JSON 规则，`ruleSearch` / `ruleBookInfo` / `ruleToc` / `ruleContent`，支持 CSS、XPath、JSONPath、JS | 不能：面向书籍章节，字段与流程完全不同，且大量规则依赖内嵌 JS |
| Tachiyomi / Mihon（扩展） | 编译好的 **APK**（Kotlin），通过扩展仓库安装 | 不能：那是 Android 插件 ABI，不是可导入的规则 |
| 异次元（图源） | Lua 脚本 | 不能：需要脚本运行时 |

所以 Reelkeep 自己定义 **schema v1**，但把每个局部都落在已有的国际标准上，降低学习成本和实现风险。

## 复用的标准

| 用途 | 标准 | 说明 |
|---|---|---|
| 规则文件的结构与校验 | [JSON Schema 2020-12](https://json-schema.org/draft/2020-12) | `docs/schema/source.schema.json`；Go 侧另有更严格的运行时校验 |
| HTML 选择器 | [CSS Selectors Level 4](https://www.w3.org/TR/selectors-4/) | 通过 goquery/cascadia 实现 |
| JSON 查询 | [RFC 9535 JSONPath](https://www.rfc-editor.org/rfc/rfc9535.html) | 通过 `github.com/theory/jsonpath` 实现 |
| 结构化元数据 | [schema.org VideoObject](https://schema.org/VideoObject) | 详情页 `<script type="application/ld+json">` 的 `name` / `duration` / `contentUrl` / `thumbnailUrl` |
| URL 模板 | [RFC 3986](https://www.rfc-editor.org/rfc/rfc3986) | 占位符替换后的地址必须能通过 `url.Parse` |
| HLS | [RFC 8216](https://www.rfc-editor.org/rfc/rfc8216) | `download.kind = "hls"`，实际下载交给 ffmpeg |
| 离线样本 | [HAR 1.2](http://www.softwareishard.com/blog/har-12-spec/) | 采集/回放 fixture 的参考格式（当前用「HTML 文件 + 列表/详情两个槽位」的简化形态） |

## 为什么不用 XPath？

- 现有解析链是 goquery（CSS），加 XPath 要再引一个引擎；
- 视频站点绝大多数是简单的 class/href 结构，CSS 够用；
- 需要结构化数据时，JSON-LD 比 XPath 更稳（站点自己维护的语义字段）。

如果将来确实需要，`field` 可以增加 `xpath`，属于 schema v2 的兼容扩展。

## 为什么不做「任意脚本」

规则一旦允许执行任意代码，就必须配套沙箱、超时、资源上限、域名白名单、供应链签名——那是一个独立的运行时项目。当前选择：

- v1：纯声明式，可校验、可 diff、可审计；
- 将来（确有需要时）：受限 JS 钩子（goja），只暴露 `fetch`/选择器/正则，无文件系统、无任意网络、15 秒超时。

## 版本化

- 源包内 `schema: 1` 对应本文档；
- 增加字段是向后兼容的（旧版忽略未知字段？——**不**，当前实现是 `DisallowUnknownFields`，所以新增字段要同时升 `schema` 并提供迁移说明）；
- `version` 是源包自身的版本，改规则就递增，诊断里会记录 `parser = "<id>@<version>"`。
