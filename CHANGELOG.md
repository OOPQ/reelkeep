# 更新日志

## 0.2.0 — 2026-11-16

### 外部源包（声明式）

- 源不再只能是内置 Go 适配器：`%APPDATA%\Reelkeep\sources\*.json` 里的源包会被加载并出现在 `/api/state.sources`（`origin: imported`）
- 格式：JSON Schema 2020-12，局部复用 CSS Selectors、RFC 9535 JSONPath、schema.org `VideoObject`、RFC 3986 占位符
- 支持 HTML 列表 / JSON 接口列表、详情页 CSS + JSON-LD、封面、HTTP/HLS 下载地址解析
- 内置安全边界：域名白名单、拒绝 `file:`/内网地址、响应大小与超时上限、重定向校验
- `410` 可声明 `refreshDetail`：下载器遇到签名过期先重解详情再重试一次
- 未知字段、坏选择器、坏 JSONPath、坏占位符在加载时一次性报全

### CLI

- `reelkeep source validate <file>`：校验源包
- `reelkeep source test <file>`：在线抓取（`--rank/--pages/--limit/--resolve/--json`）
- `reelkeep source test <file> --fixture-list/--fixture-detail`：离线回放，不需要网络

### Skill

- 新增 `skills/reelkeep-source`：指导 LLM 从样本页生成源包、校验、离线/在线测试、安装
- 已安装到本机 skills 目录，可直接在会话里调用

## 0.1.0 — 2026-11-16

首个 Go 版本，目标是「同一份数据、同一套前端、新的内核」。

### 可用

- 单二进制：Go 后端 + `go:embed` 的 React 前端，不再需要 PowerShell / Node.js
- 读取旧项目：SQLite 结构、`ledger.csv`、`deleted.json`、`config.json` 完全兼容
- 台账浏览：`/api/state`（含 `rowsRev` 增量下发）、`/api/project`、`/api/export`
- 扫盘回填：文件索引、`done`/`new` 状态迁移、孤立文件统计
- 整理：收藏、评分、已看、移入回收站、恢复（回到原状态）
- 合集：列表、新建、改名、改色、删除、加入/移出、按条目查所属合集
- 封面与缩略图接口、时间轴快照列表、诊断文件下载
- 浏览器内播放：`/media/<token>.mp4?k=`，Range / HEAD，视频流不再单起进程
- 设置：代理 / 并发 / FFmpeg 路径校验并持久化
- 兼容 PowerShell 写出的带 BOM JSON（`sources.json`、`config.json`、`deleted.json`）

### 尚未迁移（调用会明确报错）

- 抓取（`/api/scrape`、`/api/import`、`/api/refresh-url`）→ P2
- 下载（`/api/download*`）→ P4
- 时间轴快照生成、封面修复、本地导入、系统播放器/资源管理器调用 → P3
- 项目切换器（`POST /api/project/open`）

### 已知差异

- 源清单里的 `enabled` 现在真正生效；旧版清单全部启用
- 媒体目录在 Go 里用 `filepath.WalkDir`，遇到不可读文件跳过而不是整次失败
- `stats.lastScan` 目前是请求时刻（后续改为真实扫描时间）
