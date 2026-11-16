# 源包格式参考（schema v1）

机器可读版本：`docs/schema/source.schema.json`（JSON Schema 2020-12）。
实现：`internal/rule`。校验命令：`reelkeep source validate <file>`。

## 顶层

| 字段 | 必填 | 说明 |
|---|---|---|
| `schema` | 是 | 固定 `1` |
| `id` | 是 | `^[a-z][a-z0-9-]{1,40}$`，同时作为 token 前缀 `<id>@<原生id>` |
| `name` | 是 | 展示名 |
| `version` | 是 | `x.y.z`，改动后递增 |
| `author` / `description` | 否 | 作者与说明 |
| `baseUrl` | 是 | `http(s)` 绝对地址；其 host 自动进入允许名单 |
| `hosts` | 否 | 额外允许访问的域名（封面 CDN、媒体 CDN），精确匹配、不含端口 |
| `capabilities` | 是 | `scrape` `detail` `cover` `download` 的子集 |
| `defaults` | 否 | 默认请求头、UA、超时（默认 15s）、响应上限（默认 8MB）、代理（`none`/`system`） |
| `list` | 是 | 列表页规则 |
| `detail` | 否 | 详情页规则；声明 `detail` 能力时必填 |
| `download` | 否 | 下载地址规则；声明 `download` 能力时必填 |
| `auth` | 否 | 需要登录时声明：`type: cookie`、`loginUrl`、`domains`、可选 `check` |

## list

| 字段 | 说明 |
|---|---|
| `url` | 列表页模板，占位符 `{rank}` `{page}` `{baseUrl}` |
| `pageStart` | 起始页，默认 1（仅元数据，实际页码由调用方传入） |
| `pageSize` | 单页最多取多少条，默认 24 |
| `ranks` | 榜单数组：`{key,label,group?,path?}`；`path` 是该榜单的 URL 模板，覆盖 `list.url` |
| `item` | HTML 列表项 CSS 选择器 |
| `jsonPath` | JSON 列表项的 RFC 9535 路径（与 `item` 二选一） |
| `jsonld` | 从 HTML 的 schema.org JSON-LD 里取一个对象（如 `ItemList`）再套 `jsonPath` |
| `fields` | 字段取值器；HTML 用 `selector`，JSON 用 `jsonPath`；必须有 `id` |

## detail

| 字段 | 说明 |
|---|---|
| `url` | 详情页模板，占位符 `{id}` `{baseUrl}` `{pageUrl}`；列表里有完整地址时可直接写 `{pageUrl}` |
| `always` | true：列表阶段每条都抓详情（列表页没有播放地址时必须） |
| `fields` | `selector` 或 `jsonld`（JSON-LD 只能在这里用） |

## download

| 字段 | 说明 |
|---|---|
| `kind` | `http` 或 `hls` |
| `url` | 下载地址模板；留空表示用记录里的 `playable` |
| `headers` | 下载时附带的请求头，值支持占位符 |
| `excludeHeaderHosts` | 这些 host 不发送 headers（CDN 拒绝第三方 Referer 时用） |
| `onStatus` | 目前只支持 `"410": {"action":"refreshDetail","retryOnce":true}` |

## auth（登录）

需要登录才能拿到播放地址/高清时声明：

```json
{
  "auth": {
    "type": "cookie",
    "loginUrl": "/login",
    "domains": ["example.com"],
    "check": {
      "url": "/videos/latest/1",
      "loggedInContains": "退出",
      "loggedOutContains": "登录"
    }
  }
}
```

| 字段 | 说明 |
|---|---|
| `type` | 目前只支持 `cookie` |
| `loginUrl` | 登录页；绝对地址或以 `/` 开头的相对地址 |
| `domains` | 注入 Cookie 的域名（精确或子域）；缺省为 `baseUrl` 及其子域 |
| `check.url` | 可选：检测页，支持 `{baseUrl}` |
| `check.loggedInContains` | 页面包含该文本视为已登录 |
| `check.loggedOutContains` | 页面包含该文本视为未登录 |

**凭据不在源包里。** 用户在「源管理 → 登录」里粘贴 Cookie，Windows 下用 DPAPI 加密保存；
导出源包不会带出 Cookie。抓取器只在 `auth.domains` 匹配的请求上注入。

如果检测页没有稳定标记，可以省略 `check`，此时「测试登录」只确认页面可访问。

## field

| 字段 | 说明 |
|---|---|
| `selector` | CSS 选择器（在列表项节点内查找；匹配不到时会尝试节点自身） |
| `jsonPath` | RFC 9535 JSONPath（JSON 列表项以该项对象为根） |
| `jsonld` | schema.org 路径，如 `VideoObject.contentUrl`、`VideoObject.interactionStatistic[0].userInteractionCount` |
| `raw` | true 时对整个 HTML 做 `regex` 匹配（URL 藏在内联脚本里时用，结果会做 HTML 反转义） |
| `attr` | 取属性值（必须配合 `selector`） |
| `text` / `html` | 取文本 / 内部 HTML（默认 text） |
| `regex` | 对取到的值做匹配，有捕获组时取第 1 组 |
| `absolute` | 把相对地址解析成绝对地址（相对列表/详情页 URL） |
| `trim` | 默认 true |
| `default` | 取空时的兜底值 |

## 占位符

`{baseUrl}` `{id}` `{rank}` `{page}` `{playable}` `{pageUrl}` `{url}` `{token}` `{sourceId}`

未知占位符会校验失败。

## 地址解析

- `list.url` / `detail.url` / `download.url` 可以写相对路径（如 `/list/{page}.htm`），会自动按 `baseUrl` 解析成绝对地址
- 列表页是 HTML、但数据在 JSON-LD 里时，用 `jsonld` + `jsonPath`（例如 `ItemList` + `$.itemListElement[*]`）；此时字段用 `jsonPath`

## 校验规则摘要

- 未知字段直接报错（防止拼写错误被静默忽略）
- `selector` / `jsonPath` / `jsonld` 三选一
- HTML 列表不能用 `jsonPath`；JSON 列表不能用 `selector`；`jsonld` 只能用于详情
- `attr` 只能和 `selector` 一起用
- `regex`、CSS、JSONPath 都会预编译，非法即报错
- `capabilities` 与 `detail` / `download` 段必须一致
- `baseUrl` 与 `hosts` 是仅有的允许访问域名

## 最小示例（HTML + JSON-LD）

```json
{
  "schema": 1,
  "id": "example",
  "name": "Example",
  "version": "1.0.0",
  "baseUrl": "https://example.com",
  "hosts": ["cdn.example.com"],
  "capabilities": ["scrape", "detail", "cover", "download"],
  "list": {
    "url": "/videos/{rank}?page={page}",
    "ranks": [{ "key": "latest", "label": "最新" }],
    "item": "a.card",
    "fields": {
      "id": { "selector": "a.card", "attr": "href", "regex": "/video/(\\d+)" },
      "title": { "selector": ".title" },
      "cover": { "selector": "img", "attr": "src", "absolute": true }
    }
  },
  "detail": {
    "url": "/video/{id}",
    "fields": {
      "playable": { "jsonld": "VideoObject.contentUrl" },
      "duration": { "jsonld": "VideoObject.duration" },
      "cover": { "jsonld": "VideoObject.thumbnailUrl" }
    }
  },
  "download": { "kind": "http", "url": "{playable}", "headers": { "Referer": "{baseUrl}/" } }
}
```

## 最小示例（JSON API）

```json
{
  "schema": 1,
  "id": "example-api",
  "name": "Example API",
  "version": "1.0.0",
  "baseUrl": "https://api.example.com",
  "capabilities": ["scrape", "download"],
  "list": {
    "url": "/v1/videos/{rank}?page={page}",
    "jsonPath": "$.data.items[*]",
    "fields": {
      "id": { "jsonPath": "$.vid" },
      "title": { "jsonPath": "$.name" },
      "playable": { "jsonPath": "$.file" },
      "duration": { "jsonPath": "$.seconds" }
    }
  },
  "download": { "kind": "http", "url": "{playable}" }
}
```
