# 发布与签名（免费/开源方案）

现实约束先说清楚：

| 平台 | 免费可信签名 | 说明 |
|---|---|---|
| Windows | **SignPath Foundation**（开源项目免费） | 由 SignPath 的证书代签，CI 集成；不是把证书文件给你 |
| macOS | **ad-hoc 签名**（免费） | 能运行，但未公证；首次打开需右键 → 打开 |
| Linux | **SHA256SUMS + 可选 GPG**（免费） | 没有类似 Authenticode 的强制签名；GPG 由项目自己的 key 签 |

> 不存在「Let's Encrypt 代码签名」。自签名证书也不被其他机器信任，只能用于本机测试，不能解决 SmartScreen/Gatekeeper。

## Windows：SignPath Foundation（推荐）

适合公开、开源的 GitHub 项目，免费。步骤：

1. 在 <https://signpath.io/> 申请 **Foundation** 计划，安装 SignPath GitHub App 并授权本仓库
2. 在 SignPath 建项目，创建一个 signing policy（例如 `release-signing`）
3. 建 Artifact Configuration（root 为 zip，因为 `upload-artifact` 默认打包成 zip）：

```xml
<artifact-configuration xmlns="http://signpath.io/artifact-configuration/v1">
  <zip-file>
    <pe-file path="reelkeep-desktop.exe">
      <authenticode-sign/>
    </pe-file>
    <pe-file path="Reelkeep-Setup-*.exe">
      <authenticode-sign/>
    </pe-file>
  </zip-file>
</artifact-configuration>
```

4. 在 GitHub 仓库加 Secrets：

| Secret | 值 |
|---|---|
| `SIGNPATH_API_TOKEN` | SignPath 用户 API Token（submitter） |
| `SIGNPATH_ORGANIZATION_ID` | SignPath Organization ID |
| `SIGNPATH_PROJECT_SLUG` | 项目 slug |
| `SIGNPATH_SIGNING_POLICY_SLUG` | `release-signing` |
| `SIGNPATH_ARTIFACT_CONFIGURATION_SLUG` | 可选，Artifact Configuration slug |

5. 推 `v*` tag。CI 会：
   - 上传未签名 exe/installer 作为 GitHub artifact
   - 调 `signpath/github-action-submit-signing-request@v3` 等待签名
   - 用签名后的文件替换并上传最终 artifact

没配 `SIGNPATH_API_TOKEN` 时自动跳过，仍产出未签名包。

> SignPath 的证书是 SignPath Foundation 的，不是本项目的；SmartScreen 信誉需要随下载量积累，不会一签就“绿”。

## Windows：PFX 兜底（付费/自备证书）

CI 仍支持传统 PFX：

- `WINDOWS_PFX_BASE64`：`.pfx` 的 Base64
- `WINDOWS_PFX_PASSWORD`

配置后 `signtool` 会签 exe 与安装器。与 SignPath 同时配置会重复签名，建议只留一个。

## macOS：ad-hoc（免费）与 Developer ID（付费）

当前 `scripts/package-macos.sh` 做的是：

```bash
codesign --force --deep --sign - Reelkeep.app
```

- 免费，Apple Silicon 上可运行
- **未公证**，用户首次打开需要右键 → 打开，或 `xattr -dr com.apple.quarantine`
- 没有免费的可信分发路径；Developer ID + notarytool 需要 Apple Developer Program（付费）

如果以后有付费账号，在 CI 里加：

```bash
codesign --force --deep --options runtime --sign "Developer ID Application: ..." Reelkeep.app
xcrun notarytool submit ... --wait
xcrun stapler staple Reelkeep.app
```

## Linux：SHA256SUMS / GPG（免费）

Release job 会生成 `SHA256SUMS`：

```bash
cd release
sha256sum * | sort -k2 > SHA256SUMS
```

配置 `GPG_PRIVATE_KEY` 后还会生成 `SHA256SUMS.asc`：

```bash
gpg --verify SHA256SUMS.asc SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
```

GPG key 可以用 `gpg --full-generate-key` 免费生成；把公钥放到 README/keyserver 便于校验。

## 自动更新的前提

当前 Windows 桌面版已经实现「检查更新 → 下载 → SHA256 校验 → 静默安装 → 自动重启」：

- 安装包来自 GitHub Releases（HTTPS）
- SHA256 优先取 GitHub asset 的 `digest`，没有则读 Release 里的 `SHA256SUMS`
- 校验失败或缺少校验值时拒绝安装

但**没有验证代码签名**（签名暂时作为 TODO）：

- 未签名安装包在 Windows 上可能弹「未知发布者」/SmartScreen
- 威胁模型是「信任 GitHub 仓库与 Release」，适合朋友之间使用，不适合高风险分发
- 以后接入 SignPath 后，应再增加 Authenticode 验证（`WinVerifyTrust`）再启动安装器

macOS / Linux 目前只提供下载链接，不做自动覆盖安装。

