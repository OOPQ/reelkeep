# Reelkeep HTTP 契约（P0 冻结）

本文件是 `internal/api` 的验收基线。改接口时先改这里，再改实现与 `internal/api/*_test.go`。

## 约定

- 只监听 `127.0.0.1`，不对外网开放
- 请求/响应均为 JSON（`/media/*`、`/api/cover`、`/api/logo`、`/api/snapshot/image`、`/api/export` 除外）
- **业务错误**：HTTP 200 + `{"ok":false,"error":"..."}`（兼容旧版；前端看 `ok`）
- **请求体不是合法 JSON**：HTTP 400 + `{"ok":false,"error":"invalid json"}`
- 未找到的路由/文件：404；不返回 500
- `/api/state` 的 `api` 固定为 `4`，前端靠它识别旧进程

## 状态与增量

`GET /api/state` 顶层字段：

```
ok, api, rowsRev, rows?, stats, log, job, download, project, settings, sources, media, resume, metrics
```

- 首次请求（不带 `rowsRev`）返回 `rows`
- 带 `rowsRev=<当前值>` 且台账没变时不返回 `rows`
- 台账变化后 `rowsRev` 前进并重新返回 `rows`
- `rows[]` 的 34 个字段见 `internal/api/testdata/row_keys.golden`

`GET /api/events`：SSE，`Content-Type: text/event-stream`，首包 `event: hello`。

## 路由表

### 基础

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/` | SPA 入口（含 Reelkeep 标题） |
| GET | `/assets/*` | 前端静态资源；拒绝 `..` |
| GET | `/api/state` | 台账 + 统计 + 设置 |
| GET | `/api/events` | SSE |
| GET | `/api/logo` | SVG 图标 |
| GET | `/api/cover?token=` | 封面/缩略图；缺失 404 |
| GET | `/media/<token>.mp4?k=` | 本机播放，支持 Range；key 错 404 |
| GET | `/api/export` | 台账 CSV 导出 |
| POST | `/api/log` | 前端日志上报 |

### 台账写操作

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/project` | 当前项目快照 |
| POST | `/api/scan` | 扫盘回填 |
| POST | `/api/fav` | 收藏 |
| POST | `/api/rate` | 评分 0–5 |
| POST | `/api/mark` | 标记 |
| POST | `/api/watched` | 已看/待看 |
| POST | `/api/delete` | 移入回收站/删除 |
| POST | `/api/restore` | 从回收站恢复 |
| GET/POST | `/api/collections` | 合集 |
| POST | `/api/config` | 代理/并发（1–16）/ffmpeg |
| POST | `/api/project/import` | 扫描 `media/` 补条目 |
| POST | `/api/project/open` | 打开项目目录（拉起资源管理器） |
| POST | `/api/import/local` | 外部文件夹导入 |

### 下载

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/download` | 队列快照；`download.paused` 必存在 |
| POST | `/api/download` | 入队；全部失败时 `ok:false` |
| POST | `/api/download/pause` | 全局/单条暂停；空闲时 `ok:true,count:0` |
| POST | `/api/download/cancel` | 取消 |
| POST | `/api/download/clear-done` | 清理完成 |
| POST | `/api/download/reset` | 解除占用（幂等） |
| POST | `/api/download/repair` | 校验/整理 `.part`，返回 `renamed/deleted/skipped/validator` |

### 快照 / 封面

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/snapshot` | 异步 ffmpeg 抽帧 |
| GET | `/api/snapshot/jobs?token=` | 任务列表 |
| GET | `/api/snapshots?token=` | 已生成快照 |
| GET | `/api/snapshot/image?token=&name=` | 快照图片 |
| POST | `/api/snapshot/delete` | 删除快照 |
| POST | `/api/snapshot/cover` | 设为封面 |
| POST | `/api/covers/repair` | 修复封面/缩略图 |

### 源 / 抓取

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/sources` | 源列表摘要（不是完整源包） |
| GET | `/api/sources/package?id=` | 完整源包 JSON |
| POST | `/api/sources/import` | 导入/校验 |
| POST | `/api/sources/enable` | 启用/禁用 |
| POST | `/api/sources/remove` | 删除 |
| GET | `/api/sources/export?id=` | 导出源包 |
| POST | `/api/sources/test` | 抓取预览 |
| GET/POST | `/api/sources/auth*` | 手动 Cookie / 状态 / 测试 |
| POST | `/api/scrape` | 榜单抓取；`{url}` / `{urls}` 为链接抓取（批量） |
| POST | `/api/refresh-url` | 重新解析播放地址（≤20 条） |
| GET | `/api/diagnostics/scrape` | 抓取诊断列表 |
| GET | `/api/diagnostics/scrape/file?run=&name=` | 诊断文件 |

### 项目 / 桌面

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/projects` | 当前 + 最近项目 |
| POST | `/api/projects/open` | 切换项目（桌面新窗口 / 浏览器新实例） |
| GET | `/api/desktop` | 是否桌面壳 / 最大化 |
| POST | `/api/desktop/window` | 最小化/最大化/关闭 |
| POST | `/api/desktop/pick-folder` | 系统文件夹选择器 |
| POST | `/api/desktop/login` | 打开应用内登录窗口 |
| POST | `/api/desktop/login/capture` | CDP 读取登录 Cookie |
| POST | `/api/play` / `/api/reveal` | 播放 / 资源管理器定位 |
| GET | `/api/update` | 检查 GitHub Releases（会联网） |

## P0 验收对应

| 测试 | 内容 |
|---|---|
| `TestP0Acceptance`（25 项） | 首页/静态资源/路径穿越/state/cover/export/project/scan/收藏/评分/配置校验/scrape/下载/SSE/rowsRev/导入 |
| `TestHTTPContractSmoke`（41 项） | 所有主要路由存在 + 关键状态码 |
| `TestGoldenStateShape` | `row/stats/project/download` 字段基线（`testdata/*.golden`） |
| `TestScrapeDiagnosticsBaseline` | 诊断目录 `meta.json/progress.json/result.csv/scrape.log` 字段基线 |
| `TestDownloadRepair` | `.part` 校验/改名/删除 |

运行：

```powershell
go test ./internal/api/ -run 'TestP0Acceptance|TestHTTPContractSmoke|TestGoldenStateShape|TestScrapeDiagnosticsBaseline|TestDownloadRepair'
```
