# 更新日志

## 1.0.7 — 2026-11-18

### 源与抓取
- 新增源包在线索引：`sources/index.json` + 生成脚本，可在「源管理 → 可用源」一键安装/更新，安装时校验 sha256
- 源包新增 `defaults.render: browser`：用本机 Chrome/Edge 无头渲染后再解析，适合纯 JS 站点
- 源包新增 `defaults.tlsProfile`（chrome / firefox / safari）：按浏览器 TLS 指纹发请求
- 设置页新增「源索引地址」，默认指向仓库里的公共索引

### 安全
- 新增 Origin / Host 校验：跨站请求与 DNS 重绑定一律拒绝；未注册的 `/api/*` 返回 JSON 404
- 新增 `GET /api/health` 轻量探活接口

### 修复
- 分辨率筛选改为语义匹配：1080p+ / 720p+ / 4K+ / 竖屏不再返回 0 条
- 单实例文件锁 + 端口记录，避免同时启动两个进程；探活改用 `/api/health`
- 彻底删除改为先删台账再删文件；导入本地视频不再重复登记已下载文件；孤立文件统计按稳定 uid 匹配
- 下载回填、播放计数改为定向更新，避免整行覆盖；设置保存串行化

### 体验
- 队列补「已取消」Tab；合集/标签详情分页；封面选择限制 150 条
- 列表、历史、回收站、设置、源管理补错误态与重试
- 预览弹窗小屏上下排布；视频加载失败提示；快照删除加确认；抓取上限提示与后端一致

## 1.0.6 — 2026-11-18

### 源与抓取
- 核心与具体站点解耦：删除 pektino 专用数据修复、标题后缀清洗、token 归一和默认源写死
- 源包新增可选字段 `linkMatch`：链接抓取按 URL 路径正则归类，同站多源不再靠代码判断
- 未声明 `linkMatch` 的旧源包按榜单路径静态前缀归类（`/video` 不会误吞 `/videos/…`）
- qise / qise-video 源包补上 `linkMatch`
- 移除设置里的 `qise Cookie`；登录凭据统一在「源管理 → 登录凭据」填写（旧配置不再读取）

### 媒体库
- 来源筛选补上「其他」；选中后包含 `other` 与 `local`
- 服务端分页 `source=other` 同步按兜底分类过滤
- 获取数据的来源列表不再出现「其他」

### 其他
- 测试夹具与注释去掉站点名；随仓库源包的校验改为遍历 `sources/` 目录
- 源包 schema 与技能文档补充 `linkMatch`

## 1.0.5 — 2026-11-18

### 安装与源包

- 安装包内置源包，首次启动自动释放
- Windows 安装器自带 WebView2 引导程序，缺失时自动安装
- 设置页新增「自动下载 FFmpeg」
- 源管理支持导入 `.zip`，可一次导入多个源
- 构建/发布自动生成 `reelkeep-sources-<版本>.zip`

### 源

- 新增 hanime1 源
- hanime1 支持播放清单导入并自动创建合集
- hanime1 扩充为 17 个榜单/分类
- hsex 签名 HLS 地址 403/410 时自动刷新详情后重试
- pektino 旧 token 规范化为 `pektino@source_id`
- pektino 旧媒体文件自动重命名并恢复播放关联
- pektino 旧数据一次性全量刷新元数据

### 合集

- 合集列表改为卡片网格
- 新增 `/collections/:id` 详情路由
- 合集支持搜索、排序、仅看有内容
- 默认封面取第一条影片，支持自定义封面
- 新建合集改为独立弹窗
- 合集更新时间随成员变化刷新

### 媒体库

- 浮窗上一项/下一项按当前筛选和列表顺序
- 抽卡只从当前筛选结果抽取
- 「今日新增」使用本地日期，服务端分页同样生效
- 来源筛选改为读取源管理实际列表
- 修复 token 重命名后媒体文件关联丢失

## 1.0.4 — 2026-11-17

### 播放器

- 播放中切换上一项/下一项会继续自动播放
- 点击封面直接开始播放
- 上一项/下一项移入底部工具栏，不再全高覆盖画面
- 切换上一项/下一项不再关闭浮窗
- 短按空格切换播放/暂停，长按 300ms 后才进入倍速
- 暂停不再卸载 video，修复黑屏和进度异常
- 修复跨视频续播：切换/关闭前保存，进入新视频时正确恢复
- 可编辑状态下才允许修改合集/标签，去掉对勾符号

### 媒体库

- 表格封面改为高清原图
- 修复评分列与入库时间列交叠
- 评分后立即刷新，不再需要手动刷新
- 标签编辑后表格实时更新
- 表格标签支持多行，限制在行高内
- 表格标签样式与卡片统一：边框、底色、圆点
- 卡片标签移到浏览/点赞行右侧
- 卡片标签改为单行彩色 chips，超出显示 +N
- 画廊标签样式整体优化

### 队列

- 修复下载队列每次刷新重新排序
- 同一秒加入的任务按 token 稳定排序

## 1.0.3 — 2026-11-17

### 合集与标签

- 恢复并保留「合集」：合集页、合集筛选、批量加入/移出
- 新增独立「标签」体系：`tags` / `item_tags`，与合集互不影响
- 标签支持新建、改名、改颜色、删除、合并
- 打标签时可直接临时新建标签
- 表格与画廊展示标签 chips，详情弹窗可同时编辑合集和标签
- 标签支持多选交集筛选，合集筛选可叠加

### 来源

- 链接抓取按 host 自动归类到具体源
- 移除「网页」分类，新增「其他」兜底分类
- 旧 `web` 数据自动迁移到「其他」
- 来源顺序调整为：本地 → 外部源 → 其他

### 审计修复

- `/api/state` 不再返回 qBittorrent 密码和明文 Cookie
- 状态接口支持 `rowsRev` 增量，避免重复传输整库
- 取消下载立即清理 `.part` 并删除队列记录
- 新增清空失败队列
- 启动时不再恢复旧磁力任务
- 历史页支持清空历史
- 设置页支持 HTTP 下载限速
- 新增清理封面缓存
- 表头虚拟滚动偏移动态计算
- FTS5 改为按需重建

### 说明

- 磁力入口暂时关闭，已有磁力任务启动时会被取消
- 已有合集数据保留，标签使用独立数据表

## 1.0.2 — 2026-11-17

### 媒体库与历史

- 新增「历史」页：按播放日期分组、可展开收起、默认展开最近一天，封面/按钮均可播放
- 播放器第一次播放时写回 `play_count / last_play`，关闭后再次打开会重新计数
- 预览弹窗提升为全局 PreviewHost，历史页点击立即生效，不再切菜单后才弹

### 表格

- 表格改为固定布局，排序时列宽不再跳动
- 封面缩略图放大到 64×86；标题列限制宽度并截断
- 表头、横向滚动条在竖向滚动时保持可见
- 表头/行勾选框与操作列居中对齐
- 新增列显隐和拖动列宽，偏好持久化
- 超过 80 行启用表格虚拟滚动；画廊启用离屏渲染优化

### 队列与下载

- 队列卡片恢复原尺寸；修复悬浮时全部卡片一起高亮（命名 group）
- 失败条目支持单条重试
- 下载完成跳转到「已完成」，隐藏已取消标签
- 下载重试次数/间隔可在设置中调整（1–8 次、1–30 秒）
- 修复暂停/继续时的双 goroutine 竞态，续传从 `.part` 实际大小继续；全部暂停后单条继续可生效

### 源与抓取

- pektino：新增日榜（根路径），列表改用 HTML 卡片 50 条/页
- hsex：补齐最新/最热/周榜/月榜/5分钟+/10分钟+
- qise：补齐 24 个 `/videos` 分类；新增 `qise-video` 源覆盖 `/video` 单视频 15 个分类
- 「全部榜单」测试升级：列表/详情/封面/下载解析/mode 全链路，并展示每阶段结果
- 抓取器新增按 host 限流（200ms）和 429/503 退避，避免 hsex 1015 限流

### 其他

- FFmpeg 自动查找：设置、PATH、程序目录、常见安装目录、旧项目配置
- 设置页视频数/占用/封面数修复，不再显示 0
- 删除时间轴快照不再二次确认
- SQLite 增加 source/first_seen/last_play/rating 索引
- 抓取任务进度补齐 Total，不再显示 44/0

## 1.0.1 — 2026-11-16

### 修复桌面端项目切换

- 第二个桌面进程与第一个共用 WebView2 user data 目录，创建 Controller 报 `8007139f` 后崩溃，窗口不出现
  - 每个进程改用独立 WebView2 数据目录（登录凭据仍在 secrets，不依赖 WebView2 会话）
- 切到**已经打开**的项目时，不再创建第二个 Wails 窗口（`Existing` 实例没有 Server，会空指针）
  - 改为按窗口标题聚焦已有窗口；找不到（例如 CLI 起的服务）才开浏览器
- `boot.Start` 扫描 `base..base+19` 端口，复用同项目已运行实例，避免重复起服务/双写数据库
- 新增 boot 测试：同项目在相邻端口复用
- 实测：A→B 打开新窗口；重复切 B 只聚焦；同项目再次启动只聚焦，进程/端口不增加

## 1.0.0 — 2026-11-16

### P4 边界收尾

- token / 来源名路径安全：拒绝 `/`、`\`、`..`、Windows 非法字符，阻止下载路径穿越；启动时发现历史坏任务直接标记 failed
- HTTP 206 校验 `Content-Range`：起始位置不符时删除 `.part` 从头重下，避免拼出坏文件
- 410 刷新只执行一次：刷新后仍 410 直接失败，不再重复请求
- 断流续传：读取中断/不完整时自动重试（最多 2 次），从 `.part` 实际大小继续 Range
- HLS 失败清理 `.part`（不能断点续传，避免留下坏文件）
- 新增边界测试：非法路径、206 范围不匹配、410 只刷新一次

### 1.0 发布

- 功能范围完成：项目工作区、源包/登录/抓取、下载、媒体库、快照、桌面壳、安装器、CI、自动更新（未签名）
- 代码签名保留为 TODO（见 `docs/release-signing.md`）

## 0.10.0 — 2026-11-16

### Windows 自动更新（未签名）

- 设置 →「版本与更新」新增「下载并安装」（仅 Windows 桌面版）：
  - 读取 GitHub Releases 最新版，自动挑选 `Reelkeep-Setup-*.exe`
  - 优先用 GitHub asset 的 `digest` 校验 SHA256；没有则下载 `SHA256SUMS` 兜底
  - 校验通过后以 `/S` 静默启动 NSIS 安装器；安装器结束当前进程、覆盖安装，并自动启动新版本
  - 校验失败/缺 SHA256 时拒绝安装
- 其他平台仍显示「前往下载」；macOS/Linux 不做自动覆盖安装
- 未做代码签名，Windows 可能提示「未知发布者」；SignPath/GPG 方案保留为 TODO（见 `docs/release-signing.md`）
- 测试：安装包选择、下载 SHA256、SHA256SUMS 解析、Web 模式拒绝自动安装
- NSIS 静默安装成功后自动启动新版本

## 0.9.9 — 2026-11-16

### 免费/开源签名

- Windows：接入 **SignPath Foundation**（开源项目免费）
  - CI 上传未签名 artifact → SignPath 代签 → 替换 exe/installer
  - secrets：`SIGNPATH_API_TOKEN / ORGANIZATION_ID / PROJECT_SLUG / SIGNING_POLICY_SLUG / ARTIFACT_CONFIGURATION_SLUG`
  - 未配置时自动跳过，仍产出未签名包
- Linux：Release 生成 `SHA256SUMS`；配置 `GPG_PRIVATE_KEY` 后追加 `SHA256SUMS.asc`
- macOS：保持 ad-hoc 签名（免费，未公证）；Developer ID/notarization 需要付费 Apple 账号
- 新增 [docs/release-signing.md](docs/release-signing.md)，说明免费路线、SignPath 配置、GPG 校验与自动更新前提

## 0.9.8 — 2026-11-16

### macOS / Linux 桌面包

- macOS：`scripts/package-macos.sh` 生成 `Reelkeep.app` + `.dmg`（Intel / Apple Silicon）
  - Info.plist、`.icns`（从 512 PNG 生成）、ad-hoc `codesign`
  - 未公证：首次打开需右键 → 打开
- Linux：`scripts/package-deb.sh` 生成 `.deb`（amd64 / arm64）
  - 安装到 `/usr/bin`，带 `.desktop` 与图标
  - 依赖系统 `libgtk-3-0`、`libwebkit2gtk-4.1-0`、`libjavascriptcoregtk-4.1-0`
- CI 的 unix 矩阵扩展为 macOS Intel/ARM、Linux amd64/arm64，产出 `.dmg` / `.deb` / `.tar.gz`
- 不做 AppImage：会把整套 WebKitGTK 再打一遍，体积/兼容性都差；`.deb` + 系统依赖更合适

## 0.9.7 — 2026-11-16

### P0 验收基线

- 新增 `docs/api-contract.md`：冻结 `/api/*` 路由、状态码与业务错误约定
- 移植旧版 25 项 HTTP 验收为 Go 测试 `TestP0Acceptance`：
  首页/静态资源/路径穿越、state、cover、export、project、scan、收藏、评分、配置校验、scrape、下载、SSE、rowsRev、项目导入
- 新增金标字段基线 `TestGoldenStateShape` + `internal/api/testdata/*.golden`：
  固定 `rows[].34 字段`、`stats`、`project`、`download` 的字段集合
- 新增诊断基线 `TestScrapeDiagnosticsBaseline`：
  固定 `meta.json / progress.json / result.csv / scrape.log` 的目录与关键字段
- `POST /api/download` 在一条都没入队时返回 `ok:false + error`（不再假装成功）
- 现有 41 项路由冒烟、`.part` 修复测试继续保留

## 0.9.6 — 2026-11-16

- 图标改为「R.」：自绘 R 路径 + 右下圆点，替换上一版胶片盘/书签标记；同步 logo/ico/syso/安装器
- 浏览器标题改为「Reelkeep · 媒体库」
- P4：`POST /api/download/repair` 真正可用
  - 扫描 `media/**/*.mp4.part`：已有正片且更大时删除残留
  - 用 ffprobe 校验时长；没有 ffprobe 时用 ffmpeg 解一帧兜底
  - 校验通过才把 `.part` 改名为正片，无效文件保留
- P0：新增 HTTP 契约冒烟测试，覆盖 41 个 `/api` 路由（含状态码断言）
- 修复测试里 media 路由路径；测试/构建全绿
- 安装器重新打包为 0.9.6

## 0.9.5 — 2026-11-16

### 新品牌图标

- 不再沿用 pektino 的「P」图标，改为 Reelkeep 自己的标记：胶片盘 + 播放三角 + 书签（Reel + Keep）
- 同步更新：`web/logo.svg`（界面）、`build/appicon.png`、Windows `icon.ico`、`desktop/icon.png`、Windows `.syso`、安装器图标

### CI / 签名 / 更新检查

- macOS（amd64/arm64）与 Linux（amd64）桌面版加入 CI，产出 tar.gz 裸二进制（暂无 .app/.deb）
- Windows CI 支持可选代码签名：配置 `WINDOWS_PFX_BASE64` / `WINDOWS_PFX_PASSWORD` secrets 后，用 signtool 签 exe 与安装器；未配置则跳过
- 设置页新增「版本与更新」：调用 GitHub Releases 检查新版本，只提示并给下载链接，不静默安装
  - `REELKEEP_UPDATE_REPO` 可覆盖仓库（默认 `OOPQ/reelkeep`）
- 尚未做：真正的自动下载/安装更新（需要先有签名证书与发布仓库）

## 0.9.4 — 2026-11-16

### P5：Windows 安装器 + CI

- NSIS 安装器 `Reelkeep-Setup-<version>.exe`：
  - 按用户安装到 `%LOCALAPPDATA%\Programs\Reelkeep`，不需要管理员权限
  - 开始菜单 / 桌面快捷方式、卸载程序、控制面板「应用和功能」条目
  - 升级时先结束正在运行的桌面端；检测 WebView2 Runtime 并提示
  - 卸载不删除项目目录、外部源包、登录凭据
- 应用图标：从 `web/logo.svg` 生成 `build/appicon.png`、`icon.ico` 与 `desktop/rsrc_windows_amd64.syso`，exe 与快捷方式带图标
- `scripts/installer.ps1`：构建安装器；`scripts/build.ps1` 仍负责 CLI + 桌面版
- GitHub Actions（`.github/workflows/ci.yml`）：
  - Linux：前端构建 + `go test` + `go vet`
  - 五个平台 CLI 产物（windows/linux/darwin × amd64/arm64）
  - Windows：桌面版 + NSIS 安装器
  - 推 `v*` tag 自动创建 Release
- 尚未做：代码签名、自动更新、macOS/Linux 桌面包

## 0.9.3 — 2026-11-16

### 交互与源管理

- 修复：点 toast 的关闭按钮会把底下打开的弹窗一起关掉（Radix 把 toast 点击当成「点击弹窗外部」）
  - `DialogContent` 现在忽略来自 `[data-sonner-toaster]` 的 pointer/focus outside
- 源管理新增「查看完整 JSON」（`GET /api/sources/package?id=`）：
  - `/api/sources` 返回的是列表摘要；后端实际加载的是安装目录里的完整源包
  - 弹窗里可直接核对 list/detail/download/auth 等字段，确认不存在按源硬编码
- 说明：规则源由通用引擎执行；只有 `web` 是原生适配器，其余（pektino/hsex/qise 等）都是纯 JSON

### 桌面登录（读 Cookie）

- 桌面端「应用内登录」打开 Wails 登录窗口，关闭时通过 WebView2 CDP 读取 Cookie（含 HttpOnly）
- 首次登录（本地还没有凭据）关窗自动保存；已有凭据时需点「读取 Cookie」显式覆盖，避免访客 Cookie 覆盖有效登录
- CDP 仅监听本机随机端口；Web / 非桌面环境仍然手动粘贴 Cookie

## 0.9.2 — 2026-11-16

### 桌面与交互修复

- 恢复无边框自绘标题栏：项目切换 + 最小化/最大化/关闭，颜色跟界面一致；移除调试用红色块
- 快照/本地封面调用 ffmpeg 时隐藏控制台窗口（`CREATE_NO_WINDOW`），不再闪终端框
- 快照提示框：
  - 修复 Sonner toast 的 `pointer-events:none`：关闭按钮点不到、点击穿透到底下按钮 → 强制 toast 可点击
  - 同一次快照只用一条 toast（`id` 复用），生成中 → 已生成/失败原地更新，不再叠两条
  - 快照时间贴近片尾时自动前移 0.3s，避免 ffmpeg 抽不到帧
- 验证：点击 toast X 后 toast 数归零；窗口最大化/还原/关闭正常；快照在 1.7s 处生成成功

## 0.9.1 — 2026-11-16

- 项目切换器新增「浏览…」：桌面端调用系统文件夹选择器（Wails Dialog），可打开或新建项目目录
- 浏览器端保持路径输入（浏览器无法访问本机目录选择）
- 桌面端 `/api/desktop/pick-folder`：取消时返回 `ok:false / 已取消`，不弹错误

## 0.9.0 — 2026-11-16

### 项目工作区（IDEA 式）

- 「打开文件夹即项目」：空目录自动创建 `project.json + data/ + media/`；非空且不是项目的目录会被拒绝，避免误把杂物目录当项目
- 最近项目 + 项目切换器：侧边栏项目名、当前项目标记、路径输入、打开/新建、在资源管理器中定位
- 切换项目 = 新窗口/新实例：
  - 桌面壳：启动一个新的桌面进程，新窗口指向新项目
  - 浏览器：启动一个新的本机实例（随机空闲端口），前端跳转到新 URL
  - 每个项目独立端口、数据库、媒体目录，互不影响
- 单实例判断改为「按项目」：同一项目复用已有服务；不同项目并行，不再误连到别的项目
- 桌面窗口标题显示项目名；原生标题栏使用深色主题，与界面风格一致
- 说明：尝试过无边框自绘标题栏（拖动/窗口按钮），但在 2x DPI 的 WebView2 下右侧绘制异常，本版回退到原生深色标题栏

### 验证

- `config.EnsureProject`：新建、重复打开、拒绝非空非项目目录
- `boot`：同一端口启动两个不同项目 → 自动分配不同端口
- 桌面实测：打开第二个项目 → 新进程 + 8792 端口，原项目 8791 不受影响
- Web 实测：`POST /api/projects/open` → 新实例随机端口，返回 URL 指向新项目

## 0.8.0 — 2026-11-16

### P2 收尾：web 通用源 / 链接抓取 / 刷新地址

- 原生 `web` 源：任意视频页面或直链
  - 直链（mp4/m3u8）直接建条目；页面解析 `og:title/og:image/og:video`、`<video>/<source>`、JSON-LD `contentUrl`，兜底正则
  - token 为 `web@<url md5 前 12 位>`，与旧版一致；封面/时长/Referer 一并入库
  - 代理跟随应用设置（`websource.Configure`），并保留环境变量兜底
- 规则源也支持链接抓取：用列表 `id` 字段的正则从 URL 提取原生 id，再走详情解析（qise / pektino 均可用）
- `POST /api/scrape {url}`：链接抓取任务，`source` 省略时默认 `web`；可与榜单抓取共用任务/诊断
- `POST /api/refresh-url {tokens}`：最多 20 条，优先 `URLScraper` 重抓页面，其次 `Refresher`，再 `Resolve`，写回地址/headers
- CLI：`reelkeep source test <pkg> --url <page> --resolve`；`--json` 增加 `pageUrl/kind`
- 测试：web 页面解析/直链、规则源 URL 抓取、API 链接抓取任务、刷新地址
- 实测：qise 详情页链接抓取（CLI + 应用内）成功；web 直链抓取 + 刷新成功

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
