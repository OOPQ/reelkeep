# 更新日志

## 0.7.0 — 2026-11-16

### 桌面壳（Wails v3）

- 新增 `desktop/`：Wails v3.1 + WebView2，窗口加载本机 loopback 服务（`http://127.0.0.1:<port>/`）
- 不生成 Wails 绑定，前端仍然只走 HTTP；业务/API/前端与 CLI 完全同一套
- `CGO_ENABLED=0` 即可构建（Windows），产物 `reelkeep-desktop.exe`（约 19MB，含内嵌前端）
- 单实例：已有服务在跑时，窗口直接指向它，不重复启动、不在退出时关掉别人的服务
- `internal/boot`：把「项目 → 数据库 → 源 → API → 下载器 → HTTP」抽成 CLI/桌面共用启动器
- `scripts/build.ps1` 同时产出 `reelkeep.exe` 与 `reelkeep-desktop.exe`；`-All` 额外产出桌面版

### 验证

- `internal/boot` 测试：启动 → `/api/state` → 单实例识别 → 关闭后可重新启动
- 桌面进程实测：窗口真实渲染前端（WebView2 进程拉起）、API 正常
- 全量 `go test ./...` / `go vet` 通过

### 未完成

- NSIS 安装器（本机未安装 NSIS）、代码签名、自动更新
- macOS / Linux 桌面包（需要各自平台工具链与 WebKitGTK）
- CI 矩阵

## 0.6.0 — 2026-11-16

### P3 收尾

- **项目 / 本地导入**：`POST /api/project/open` 打开项目目录；`POST /api/project/import` 扫描 `media/` 补条目；`POST /api/import/local` 把外部文件夹的 mp4 移动到 `media/local` 并入库（同名同大小去重）
- **封面修复**：`POST /api/covers/repair` 先给本地视频抽帧（最多 3 个），再按源解析封面并下载；404/空图写 `.missing`，避免反复重试
- **缩略图**：用纯 Go `golang.org/x/image` 生成 120px 缩略图（不再依赖 Windows Shell）
- **时间轴快照**：`POST /api/snapshot`（异步 ffmpeg，最多 2 个并发）、`GET /api/snapshot/jobs`、`POST /api/snapshot/delete`、`POST /api/snapshot/cover`
- **快照当封面**：`data/cover-map.json`，`/api/cover` 会优先返回指定快照
- **播放 / 定位**：`POST /api/play`（系统播放器、仅计数、定位）、`POST /api/reveal`（资源管理器 `/select`）；播放次数与 `last_play` 写回台账
- 封面下载有域名解析与内网拦截（IPv4 优先）

### 测试

- 项目导入幂等（第二次全部跳过）
- 本地文件夹导入：移动、入库、源文件消失
- 封面修复 + 缩略图生成（注入图片下载）
- 快照：创建 → 任务完成 → 设为封面 → `/api/cover` 返回快照 → 删除清 cover-map
- 播放计数（countOnly）写回 `play_count/last_play`

## 0.5.0 — 2026-11-16

### 下载器（P4 基础版）

- 队列落库 `download_tasks`：重启后 `downloading` 自动回到 `queued`，不会出现幽灵任务
- HTTP 断点续传：`Range` 续传、206 追加、`416` 后从头重来、`.part` 完成后原子改名
- HLS 走 ffmpeg；失败且声明了 `410` 刷新时，先重解详情再重试一次
- 暂停/继续（全局 + 单条）、取消（删除 `.part`）、清理完成记录、解除占用
- 并发：全局 `concurrent` + 每源上限 2；下载请求带浏览器 UA
- 网络：**IPv4 优先**拨号（本机 IPv6 常无路由）；302 跨域重定向保留 Referer（CDN 热链校验）
- 完成后回填台账：`file_name/file_path/file_size/file_date`、`status=done`，队列页与媒体库即时可见
- 旧版 `.queue.json` 首次启动自动迁移（改名为 `.queue.json.migrated`）
- `POST /api/download` 支持失败重试；`/api/state.download` 返回队列快照
- 说明：`/api/download/repair` 目前是空操作（旧版用 ffprobe 判断 `.part` 是否完整，后续补）

### 测试

- HTTP 续传（预置半个文件，校验 Range 与最终 SHA256）
- 410 刷新（假源 + 假 CDN，验证刷新后重试成功）
- 取消（阻塞服务，验证状态与 `.part` 清理）
- 旧 `.queue.json` 迁移

## 0.4.0 — 2026-11-16

### 登录源（手动 Cookie）

- 源包新增 `auth` 段（`type: cookie`、`loginUrl`、`domains`、可选 `check`）
- 「源管理」每个需要登录的源多出「登录」面板：打开登录页、粘贴 Cookie、保存并测试、清除
- 凭据保存在 `%APPDATA%\Reelkeep\secrets\<source>.json`，Windows 用 **DPAPI** 加密（绑定当前账户）；导出源包不会带出 Cookie
- 抓取器按 `auth.domains` 自动注入 Cookie；下载地址解析标记 `UseAuth`，P4 下载器会带上同一份凭据
- 旧版项目 `config.json` 里的 `qiseCookie` 首次启动自动迁移到加密凭据（只迁一次）
- 新增 `sources/qise.json`：列表 + 详情 + 签名 mp4 + 410 刷新详情；无登录也能抓免费视频，登录后用于高清/会员内容

### 抓取器健壮性

- 读到 `</html>` 就结束：qise 这类「发完正文不关连接」的站点不再卡死
- 每个请求独立超时（`defaults.timeoutSec`），响应头 20s；慢响应重试一次
- 启用 Cookie Jar（保留 JSESSIONID）；响应过短（如 204）自动重试一次
- 失败原因带上下文（列表页/详情页 URL）

### 修复

- 导入源与内置清单同 id 时，`Manifest()` 之前返回内置清单，导致导入源的榜单被判为未知；改为导入优先
- 抓取诊断目录补回 `data\diagnostics\scrape\<run>\` 层级（之前少了 `scrape`）
- 中文源名/榜单名在诊断 run 目录里安全化

### 测试

- secrets：加密落盘、明文不出现、非法 id/空 Cookie/超长 Cookie、换行清理
- rule：`auth` 校验、`raw` 正则 + HTML 反转义、`{pageUrl}` 详情模板、随附三个源包始终可加载
- manager：登录凭据保存/清除/状态展示

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
