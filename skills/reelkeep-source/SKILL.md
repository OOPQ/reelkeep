---
name: reelkeep-source
description: Generate and validate a Reelkeep source package (declarative JSON) for a video site — list/detail extraction with CSS or JSONPath, schema.org JSON-LD, allowed hosts, HTTP/HLS download resolution — then test it with fixtures or live and install it. Use when adding a new source to Reelkeep, fixing a broken selector, or reviewing a contributed source.
---

# Reelkeep 源包生成

用声明式 JSON 描述一个视频站点的抓取逻辑，交给 Reelkeep 解释执行。
**不写代码、不执行脚本**：源的表达力边界就是 `docs/schema/source.schema.json`。

工作目录：本 skill 的 `references/`、`examples/`、`scripts/`。
参考实现：`D:\project\reelkeep\internal\rule\testdata\demo.json`（离线 fixture 可跑）。

## 先判断能不能做（不做会白忙）

| 情况 | 结论 |
|---|---|
| 列表/详情是静态 HTML 或公开 JSON 接口 | ✅ 适合 |
| 需要登录 / 会员 / 付费才能看到播放地址 | ❌ 不做（合规与稳定性都不划算） |
| 需要 JS 渲染、签名算法、验证码 | ❌ 当前格式不支持；退回 Go 适配器 |
| 站点有 DRM | ❌ 不做 |

判断方法：`curl.exe -sL --max-time 20 <url>` 看返回里有没有真实数据。
很多站点（pektino 就是）在 `<script type="application/ld+json">` 里直接给了视频对象，优先用它。

## 工作流

### 1. 采样本

```powershell
curl.exe -sL --max-time 20 "https://site.example/list/latest?page=1" -o list.html
curl.exe -sL --max-time 20 "https://site.example/video/123"      -o detail.html
```

看三件事：

- 列表项的共同容器（class / 标签）
- 每条的原生 id 在哪个属性/字段里（通常 `href="/video/123"`）
- 播放地址在详情页的哪里：`<source src>`、`<video src>`、JSON-LD `contentUrl`、或某个 JSON 接口

### 2. 写源包

从 `examples/html-source.json` 或 `examples/json-api-source.json` 复制，改这几处：

- `id`：小写字母/数字/连字符，2–41 位
- `baseUrl`：站点根地址
- `hosts`：封面/媒体 CDN 的域名（**不写就会拒绝访问**）
- `list.url`：`{rank}` `{page}` 占位符
- `list.item` + `list.fields`：CSS 选择器，字段在列表项节点内查找
- `detail.fields`：优先用 `jsonld`（schema.org `VideoObject`），其次 CSS
- `download`：`kind` 是 `http` 或 `hls`，`url` 用 `{playable}` 或模板

字段取值器只有三种：`selector`（CSS）、`jsonPath`（RFC 9535）、`jsonld`（schema.org）。
字段名固定这套：`id` `title` `cover` `playable` `pageUrl` `duration` `res` `views` `likes` `uploaded`。

### 3. 校验

```powershell
.\scripts\validate.ps1 -Path .\source.json
# 或
reelkeep.exe source validate .\source.json
```

一次会列出所有问题，逐条改完再跑。

### 4. 离线测试（不需要网络）

```powershell
reelkeep.exe source test .\source.json --fixture-list .\list.html --fixture-detail .\detail.html --limit 2 --resolve
```

看输出：token 是否 `id@原生id`、标题/时长/播放地址是否合理、`resolve` 的 mode 与 headers 是否正确。

### 5. 在线测试

```powershell
reelkeep.exe source test .\source.json --rank latest --limit 3 --resolve
```

只抓 3 条验证，不要一上来抓整页。

### 6. 安装

```powershell
.\scripts\install.ps1 -Path .\source.json
```

会复制到 `%APPDATA%\Reelkeep\sources\<id>.json`。重启 Reelkeep 后源出现在「获取数据」的下拉里。

## 常见错误 → 修法

| 报错 | 原因 | 修法 |
|---|---|---|
| `域名不在白名单` | 封面/媒体 CDN 没声明 | 把域名加进 `hosts` |
| `CSS 选择器不合法` | 选择器语法错 | 用浏览器 DevTools 的 `Copy selector` 对照 |
| `缺少 id` | `list.fields.id` 没提取到 | 确认 `href` 属性名、正则捕获组 |
| `jsonPath 没有匹配到任何项` | 接口结构变了 | 先 `curl` 看真实 JSON，再核对路径 |
| `id 含非法字符` | id 里带 `/` `?` `%` | 用正则只取 id 部分 |
| `JSON 页面里不能用 selector` | 列表是 JSON 却用了 CSS | 改成 `jsonPath` 字段 |
| `HTML 页面里不能用 jsonPath` | 列表是 HTML 却用了 JSONPath | 改成 `selector`，或详情页用 `jsonld` |
| `未知占位符` | 模板写了不存在的 `{xxx}` | 只用 `{baseUrl}` `{id}` `{rank}` `{page}` `{playable}` `{pageUrl}` `{url}` `{token}` `{sourceId}` |
| `源没有榜单` | `--rank` 不在 `list.ranks` | 用默认榜单或补上 ranks |

## 稳定性规则（重要）

1. **选择器要抗改版**：优先 `[data-id]`、`a[href*="/video/"]`，少用 `div:nth-child(3) > span`。
2. **id 必须稳定**：不要用标题或序号当 id，改版后会产生重复条目。
3. **封面用绝对地址**：`"absolute": true`，否则会拼成列表页相对路径。
4. **播放地址可能在详情页**：列表页没有就设 `detail.always: true` 或让 playable 为空（会自动补详情）。
5. **CDN 拒绝第三方 Referer**：把域名写进 `download.excludeHeaderHosts`（如 `video.twimg.com`）。
6. **签名地址会过期**：`onStatus.410.action = "refreshDetail"` 会让下载器遇到 410 先重解详情再重试一次。
7. **不要一次抓全部页**：先 1 页 3 条验证，再放大。

## 交付前自检

- [ ] `source validate` 无错误
- [ ] 离线回放能出 ≥2 条，token 唯一
- [ ] 在线测试 ≥3 条，`playable` 非空且可播放
- [ ] `--resolve` 返回的 mode（http/hls）与站点一致
- [ ] `hosts` 覆盖了封面与媒体域名
- [ ] 没有把登录/付费页当成列表页
- [ ] `id`、`name`、`version` 有意义，`version` 在改动后递增

## 引用标准

- JSON Schema 2020-12（本格式用）
- CSS Selectors Level 4（`selector`）
- RFC 9535 JSONPath（`jsonPath`）
- schema.org `VideoObject`（`jsonld`）
- RFC 3986（URL 模板）
- RFC 8216（HLS）

详见 `references/schema.md` 与 `references/standards.md`。
