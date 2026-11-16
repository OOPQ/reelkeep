# 更新日志

## 0.3.0 — 2026-11-16

### 抓取任务

- `POST /api/scrape` 真正可用：单任务、后台执行、进度、`POST /api/job/cancel` 取消
- 抓取结果按 token 合并：新条目入库；旧条目只覆盖抓取字段，状态/收藏/评分/备注/文件字段一律保留
- 默认跳过已彻底删除与回收站条目（可传 `skipDeleted: false`）
- 每轮落诊断：`data\diagnostics\scrape\<run>\` 下 `result.csv` / `meta.json` / `progress.json` / `scrape.log`
- `last_seen`、`rank`、`page` 随抓取更新；`rowsRev` 前进，前端会自动刷新

### 源管理

- 新页面「源管理」：导入（文件/粘贴）、校验错误逐条展示、启用/禁用、测试预览、导出、删除
- `GET /api/sources`、`POST /api/sources/import|enable|remove|test`、`GET /api/sources/export`
- 导入时校验、同 id 需显式覆盖、与原生实现冲突时拒绝、安装状态与最近运行结果持久化
- 禁用的源不出现在「获取数据」下拉里；同 id 的导入源覆盖内置清单，不会出现两条
- CLI：`source install|list|remove|export`

### 源包能力

- `list.jsonld`：从 HTML 的 schema.org `ItemList` 取列表（pektino 用这个，比 CSS 稳）
- 列表/详情/下载地址统一解析为绝对 URL（模板可以写 `/list/{page}`）
- 同一轮抓取按 token 去重（列表页导航/推荐位重复链接不会重复入库）
- `defaults.proxy`：`system` 跟随 `HTTP(S)_PROXY`；显式代理优先；本地代理地址不再被 SSRF 规则误拦
- 随仓库提供两个已验证的外部源包：`sources\pektino.json`（周/月/总榜，http）、`sources\hsex.json`（最新，hls）

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
