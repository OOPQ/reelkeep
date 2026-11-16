# 外部源导入评估：能不能像漫画软件那样导入爬取逻辑？

日期：2026-11-16
状态：设计评估，未实施

## 一句话结论

**技术上完全可行，而且 `Source` 接口已经是现成的接缝；但现在不该做，而且不应做成「任意脚本」那种形态。**

正确的顺序是：先把四个内置源用 Go 写完（P2）→ 用它们反推出一套**声明式规则格式** → 再开放「导入源包」。规则覆盖不了的少数站点，加**受限 JS 钩子**，但永远不执行任意二进制/系统脚本。

只有当「别人也会写源」这个前提成立时，这套东西才划算；如果只是你自己加源，直接写一个 Go 适配器是 2~4 小时，而规则引擎要 3 周。

---

## 一、「外部导入」有三种含义，成本差一个数量级

| 含义 | 例子 | 对本项目 |
|---|---|---|
| **A. 导入爬取规则/脚本** | Legado/阅读 的 JSON 书源、异次元的 Lua 源 | 可行，需要设计格式（本文重点） |
| **B. 导入编译好的扩展** | Tachiyomi / Mihon 的扩展其实是 APK（Kotlin），不是规则文件 | 不适用：那是 Android 插件体系，不是「导入」 |
| **C. 导入源目录/仓库** | 订阅一个 JSON 列表，自动更新 | A 之上的分发层，安全风险最高，最后做 |

很多人说的「漫画软件导入图源」其实是 A；Tachiyomi 那套是 B，靠改 APK 签名和扩展仓库，跨平台无从谈起。

## 二、现有代码的接缝

Go 版已经有：

```go
type Source interface {
    ID() string
    Capabilities() []string
    Scrape(ctx, req, emit func(model.Row) error) error
    HealthCheck(ctx) error
}
type Detailer interface{ Detail(ctx, id) (model.Row, error) }
type Coverer interface{ Cover(ctx, r) (string, error) }
type DownloadResolver interface{ Resolve(ctx, r) (DownloadSpec, error) }
```

一个「导入源」无非是 `Source` 的另一种实现：`RuleSource` 读 JSON，解释执行。**API 层、下载器、前端都不需要改**——这正是当初把能力拆成接口的价值。

注册表也已经是「构造函数注册 + sources.json 控制启用/顺序」，导入源只需在启动时额外加载一个目录。

## 三、四个内置源里，规则能表达多少？

| 源 | 关键逻辑 | 纯规则能表达？ |
|---|---|---|
| pektino | 榜单页正则取 id；详情 JSON-LD；`video.twimg.com` **不带 Referer**，其他 CDN 带 | 能（需要「按 host 排除请求头」） |
| hsex | 列表 `background-image` / `<var>` 时长；详情 `og:` + `<source>` m3u8 | 能 |
| web | 任意页面取 `og:` / `<video>` / 第一个 mp4 | 能 |
| qise | Cookie、签名 m3u8 几分钟过期、410 后**重新解详情页刷新**、优先标清 `/video/view` | 大部分能（「410 重解详情重试一次」可作为声明式修复动作）；标清偏好要靠规则里的候选地址 |

结论：**规则能覆盖 3.5/4**，前提是格式里包含「按 host 的请求头」「失败后重解详情一次」「多候选下载地址」这三个特性。这比多数人预想的多——因为在 Reelkeep 里，源只负责「列表→详情→规范化字段→下载地址」，不负责下载本身。

## 四、三种实现方案对比

| 方案 | 表达力 | 安全 | 作者门槛 | 成本 | 结论 |
|---|---|---|---|---|---|
| 声明式规则（JSON + CSS/JSONPath/正则 + 少量动作） | 中 | 高 | 低（会写 CSS 就行） | 15–20 人日 | **推荐 v1** |
| 规则 + 受限 JS 钩子（goja） | 高 | 中 | 中 | +15 人日 | 确实需要时再加 |
| 独立进程插件（JSON-RPC/stdin） | 高 | 高 | 高（要会写程序） | 25 人日+ | 不做 |
| WASM 插件（wazero） | 高 | 高 | 高（要工具链） | 25 人日+ | 不做 |
| Go plugin `.so` | 高 | 低 | 高 | — | **Windows 不支持，排除** |
| 任意 PowerShell/批处理钩子 | 无限 | 无 | 低 | 低 | **绝对不做** |

关键取舍不是「能不能执行任意代码」，而是「**把作者限制在数据描述里，还是允许他写逻辑**」。前者可校验、可 diff、可审计；后者必须有沙箱和超时。

## 五、规则格式草案（v1）

```json
{
  "schema": 1,
  "id": "example",
  "name": "Example",
  "version": "1.0.0",
  "minApp": "0.3.0",
  "baseUrl": "https://example.com",
  "hosts": ["cdn.example.com"],
  "defaults": {
    "headers": { "Referer": "{baseUrl}/" },
    "ua": "Mozilla/5.0 …"
  },
  "capabilities": ["scrape", "detail", "cover", "download"],
  "list": {
    "url": "/videos/{rank}?page={page}",
    "pageStart": 1,
    "pageSize": 24,
    "ranks": [{ "key": "latest", "label": "最新" }],
    "item": "css:.video-card",
    "fields": {
      "id":    { "attr": "href", "regex": "/video/(\\w+)" },
      "title": { "css": ".title", "text": true },
      "cover": { "css": "img", "attr": "src", "absolute": true },
      "duration": { "css": ".duration", "text": true }
    }
  },
  "detail": {
    "url": "/video/{id}",
    "fields": {
      "title": { "css": "h1", "text": true },
      "playable": { "css": "source#video-source", "attr": "src", "absolute": true }
    }
  },
  "download": {
    "kind": "hls",
    "headers": { "Referer": "{baseUrl}/" },
    "excludeHeaderHosts": ["video.twimg.com"],
    "onStatus": { "410": { "action": "refreshDetail", "retryOnce": true } },
    "candidates": [
      { "url": "/video/view/{id}", "selector": "source", "prefer": true }
    ]
  }
}
```

字段取值只允许几种原子操作：`text` / `attr` / `html` / `jsonPath` / `regex` / `absolute` / `trim` / `prefix`。**不引入循环、变量、条件表达式**——一旦需要这些，就说明该写 Go 或 JS 了。

启动时全量校验（JSON Schema + 域名合法性 + 选择器语法），坏规则直接拒绝加载并给出源 id 与字段路径。

## 六、什么时候才加 JS 钩子

当出现下面任一情况，规则就不够了：

- 需要签名（时间戳 + 哈希）才能拿到地址；
- 需要多步请求（先拿 token 再换播放地址）；
- 需要解析加密/混淆的 JSON；
- 需要站点特定的分页终止条件。

这时加 `hooks.scrape`（goja，纯 Go，无 cgo），API 只给：

```js
function scrape(ctx) {
  const html = ctx.get(ctx.base + "/list/" + ctx.page);
  return ctx.$$(".card").map(el => ({
    id: ctx.regex(el.attr("href"), /video\/(\w+)/),
    title: el.text(".title")
  }));
}
```

`ctx.get` 只能访问 allowlist 域名，超时 15 秒、响应上限 8MB、无文件系统、无 `require`、无环境变量。宁可少一个源，也不开一个能读写本机的口子。

## 七、安全清单（比功能更重要）

| 风险 | 对策 |
|---|---|
| 规则把数据发到攻击者服务器（封面 URL 带 token） | 只允许 `baseUrl` + `hosts` + `coverHosts` 的域名；其它一律拒绝 |
| SSRF（读内网/本机服务） | 拒绝 `127.0.0.0/8`、`169.254/16`、`10/8`、`192.168/16`、`172.16/12`、`file:`、`data:` |
| 通过下载地址把本机文件喂给 ffmpeg | ffmpeg 输入只接受 http(s)，且过同一套 allowlist |
| 资源耗尽 | 单请求超时、总抓取超时、响应大小上限、每源并发上限 |
| 供应链（订阅源自动更新） | v1 只支持手动导入；导入时显示域名清单 + sha256；**不做静默热更新** |
| 合规 | 导入源只是规则，不内置站点；文档明确「用户自行确认内容合法性」，与现有 README 一致 |

## 八、存储与 UI

- 内置源：`configs/sources/*.json`（随二进制，只读）
- 导入源：`%APPDATA%\Reelkeep\sources\<id>.json` + `sources.index.json`（启用/顺序）
- 源管理页：内置/导入分栏，显示能力、域名、最近成功率、最后解析错误；支持启用、删除、导出、从文件/粘贴导入
- 调试：`reelkeep source test <file> --rank latest --limit 5`，打印规范化行与耗时；再加 `--fixture` 回放离线样本
- 诊断：每条记录带 `parser = "<id>@<version>"`，与 P0 的 run_id 一起定位「哪一轮、哪版规则」

## 九、成本与排期

| 项 | 人日 |
|---|---|
| 规则 Schema + 加载/校验/解释器（HTML/JSON/正则） | 12–15 |
| 下载动作（headers/候选/410 重试）接入下载器 | 3–5 |
| CLI `source test` + fixture 回放 | 3 |
| 源管理 UI（导入/导出/启用/诊断） | 4–5 |
| JS 钩子（仅当需要） | 12–15 |
| 源目录订阅（仅当需要） | 8–10 |

**建议插入点：P4 下载器完成之后（约 2~3 周后）**，理由：

1. 规则格式必须能从四个真实适配器里「反推」，否则设计出来的是想象中的需求；
2. 下载器决定了 `DownloadSpec` 的最终形状，规则里的候选地址/重试动作要跟它对齐；
3. 现在做，等于在 P2 还没验证接口时先冻结一个对外的格式，后面每改一次都要兼容。

## 十、我的建议

- **做**：P2 内置源 → 抽规则 → P6 声明式源包（含导入/导出/测试/诊断）。
- **同时保留**：内置源继续用 Go 写。规则不是用来替代内置源的，是用来让「第五个、第六个简单站点」不需要发版。
- **不做**：APK 扩展兼容、任意脚本执行、自动订阅更新、源市场。
- **先回答一个问题**：这套东西是给「其他人」用的，还是只为你自己加源？如果是后者，一个 Go 适配器 2~4 小时，规则引擎永远回不了本。

## 附：判断流程图

```text
新站点要接入
  ├─ 只给自己用？ ──是──→ 写 Go 适配器（2~4h）
  └─ 否
      ├─ 列表/详情是静态 HTML 或 JSON？
      │    ├─ 是 ──→ 写规则 JSON（30 分钟），导入
      │    └─ 否 ──→ 需要签名/多步请求？
      │             ├─ 是 ──→ JS 钩子（若已实现）或退回 Go
      │             └─ 否 ──→ 规则 + 正则字段
      └─ 站点有登录墙/付费墙？ ──→ 不做（合规与稳定性都不划算）
```
