# 架构与迁移决策

日期：2026-11-16
状态：P1 已落地

## 为什么重写

旧版是 PowerShell 5.1 + System.Data.SQLite + 单线程 `TcpListener`。能用，但三条结构性限制越来越贵：

1. **单线程服务**：长任务只能靠「一次最多 20 条 / 40 条」的硬上限和另起进程（媒体服务）绕开。
2. **运行时脆弱**：PS 5.1 专属、.NET Framework 版 SQLite、UTF-8 BOM 与编码坑、x64 限定。
3. **难测试**：源适配器的解析逻辑只能靠真实站点冒烟。

这不是「语言偏好」，是能力边界。因此换成 Go，但**不是重写产品**。

## 什么保持不变（硬契约）

| 契约 | 内容 |
|---|---|
| HTTP | `/api/*` 路径、JSON 字段、`api = 4`、`rowsRev` 增量下发全部照旧 |
| 前端 | 同一份 React 代码，零改动（只建议后续把 `rowsRev` 用起来） |
| 数据库 | `items` 34 列、`collections`、`deleted_tokens` 结构不变，只加列 |
| 导出 | `ledger.csv` 表头、列顺序、UTF-8 BOM + CRLF |
| 身份 | 默认源裸 token；其他源 `<source>@<id>`；大小写不敏感匹配文件名 |
| 媒体 | `media/<source>/<token>.mp4`；递归 4 层；去掉 ` (1)` 后缀 |
| 删除 | 回收站记 `recycled_from`，恢复回原状态；彻底删除写 `deleted.json` + `deleted_tokens` |

## 关键取舍

### 不做双 transport

Wails 只作为将来的窗口与打包壳；前端始终走 HTTP。**不生成 service 绑定**，否则测试面翻倍、收益为零。
P1 甚至没有 Wails：先证明「单 exe + 浏览器」可用，壳是低成本动作。

### 不做 Chromium 渲染

四个源全是静态 HTML/JSON（JSON-LD、正则、`data-src` m3u8），没有 JS 渲染需求。
`Renderer` 接口保留，实现推迟；不引入 150MB Chromium 和 CI 成本。

### 不重写 HLS

下载继续调用 ffmpeg（HLS）与 curl/aria2c（HTTP）。所以「单二进制」的实际承诺是「exe + 可选 ffmpeg」，不是零外部依赖。

### 源注册表：构造函数注册 + 配置控制

`Sources.json` 仍决定启用、顺序、榜单；Go 只注册构造函数。加源/关源不需要改代码，这是旧版就有的优点，保留。

### BOM 兼容

旧版 PowerShell 写出的 `sources.json`、`config.json`、`deleted.json` 带 UTF-8 BOM，`encoding/json` 会直接报错。
统一走 `internal/jsonx` 去除 BOM；这不是洁癖，是实际踩到的第一个迁移 bug。

## 源接口

```go
type Source interface {
    ID() string
    Capabilities() []string
    Scrape(ctx, req, emit func(model.Row) error) error
    HealthCheck(ctx) error
}

type Detailer interface { Detail(ctx, id) (model.Row, error) }
type Coverer interface { Cover(ctx, r) (string, error) }
type DownloadResolver interface { Resolve(ctx, r) (DownloadSpec, error) }
```

`Scrape` 用 `emit` 流式产出，而不是返回整片切片：列表→详情→入库可以重叠，也便于做增量。
`DownloadResolver` 最终会替换旧版 `download.ps1` 里的 4 处 `if (source -eq 'qise')`。

## 外部源包（P6）

内置源是 Go 代码；外部源是声明式 JSON，由 `internal/rule` 解释执行。两者都实现同一个 `Source` 接口，
所以 API、下载器、前端不需要区分「内置还是导入」。

- 格式：`docs/schema/source.schema.json`（JSON Schema 2020-12）
- 局部标准：CSS Selectors、RFC 9535 JSONPath、schema.org `VideoObject`、RFC 3986 占位符、RFC 8216 HLS
- 安全边界：`baseUrl` + `hosts` 白名单、拒绝 `file:` 与内网地址、8MB/15s 上限、重定向逐跳校验
- 加载：`%APPDATA%\Reelkeep\sources\*.json`，坏文件只影响自己；启用状态与最近运行结果写在 `sources.index.json`
- 管理：`/api/sources`（列表/导入/启用/删除/导出/测试），界面在「源管理」页
- 抓取：`/api/scrape` 起后台任务，结果按 token 合并进台账，每轮落 `data\diagnostics\scrape\<run>\`
- 工具：`reelkeep source validate|test|install|list|remove|export`，支持离线 fixture 回放
- Skill：`skills/reelkeep-source`，指导 LLM 从样本页生成源包

仓库自带 `sources/pektino.json`、`sources/hsex.json`、`sources/qise.json` 三个已验证的源包（外置，不随二进制自动加载）。

## 登录与凭据

分三层，当前实现第一层：

1. **手动 Cookie（已实现）**：用户在自己的浏览器登录，复制 `Cookie` 请求头，粘贴到「源管理 → 登录」。应用只负责安全保存与注入，不接触账号密码。
2. **一键登录（计划）**：用 CDP 拉起一个专用 Chrome 配置目录（`--user-data-dir=%APPDATA%\Reelkeep\login\<source>`），用户在里面登录后，通过 `Network.getAllCookies` 取回该源域名的 Cookie，再走同一套加密存储。需要引入一个 WebSocket 客户端依赖。
3. **内嵌 WebView（P5）**：Wails/WebView2 里直接开登录页，用 `ICoreWebView2CookieManager` 取 Cookie。体验最好，但要等桌面壳落地。

设计约束：

- 源包只声明 `auth`（类型、登录页、域名、可选检测页），**永远不含凭据**
- 凭据存 `%APPDATA%\Reelkeep\secrets\<source>.json`；Windows 用 DPAPI，非 Windows 暂用 0600 文件（发布前应换系统钥匙串）
- 注入范围：`auth.domains`，缺省为 `baseUrl` 及其子域；下载地址解析标记 `UseAuth`，下载器（P4）带同一份凭据
- 导出源包、诊断文件、日志都不包含 Cookie
- 合规：只复用用户自己账号已有的权限，不代填密码、不绕过会员墙/DRM

## 数据流

```text
启动 → 打开 data.db（WAL，单写者）
     → 加载 items / deleted_tokens / deleted.json
     → 扫盘：media → file_* / status（目录不可达时什么都不改）
     → HTTP 服务 + 嵌入前端

写操作 → 先写 DB，再更新内存行，rev/rowsRev 前进
扫盘   → 只落真正变化的行（每 5 分钟或手动触发）
```

## 下载器（P4）

- 队列表 `download_tasks`：状态、字节数、错误、时间戳；启动时 `downloading → queued`
- 调度：全局 `concurrent` + 每源上限 2；队列按加入时间
- HTTP：`Range` 续传、206 追加、`416` 从头、`.part` 完成后原子改名
- HLS：ffmpeg `-headers` 带 Referer/Cookie；失败且 `RefreshOn410` 时先 `Refresher` 再试一次
- 认证：任务只存 `UseAuth` 标记，Cookie 由 secrets 在发请求时注入，不进任务表
- 完成：写回 `items` 的文件字段和 `status=done`，并通过 `onRow` 刷新 API 内存行
- 旧版 `.queue.json` 首次启动迁移并改名

## 后续阶段

- **P2** 源适配器：pektino → hsex → qise → web；离线 fixture 回放 + 在线冒烟；诊断目录落原始响应
- **P3** 封面/缩略图（ffmpeg 取帧替代 Windows Shell）、快照、导入、播放器调用
- **P4** 下载器：队列落库、Range 续传、410 刷新签名、per-source 并发
- **P5** 打包与 CI：Windows 单文件、macOS 公证、Linux AppImage；三平台各自验收

## 明确不做

- Android 与 LAN 客户端（各端独立本地库；Hub 模式以后再评估）
- 规则 DSL / parser 插件系统（4 个源不值得）
- 指标看板 / 队列中间件（单用户单机）
