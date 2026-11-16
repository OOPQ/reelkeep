# 构建脚本：产出单二进制（内嵌 web/dist）
#
# 用法：
#   .\scripts\build.ps1                    # 本机 Windows 版
#   .\scripts\build.ps1 -All               # 交叉编译五平台到 dist\
#   .\scripts\build.ps1 -Version 0.2.0     # 指定版本号
param(
    [string]$Version = '',
    [switch]$All
)

$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    $go = (Get-Command go -ErrorAction SilentlyContinue).Source
    if (-not $go) {
        foreach ($p in @('D:\software\go\bin\go.exe', "$env:ProgramFiles\Go\bin\go.exe", "$env:LOCALAPPDATA\Programs\Go\bin\go.exe")) {
            if (Test-Path $p) { $go = $p; break }
        }
    }
    if (-not $go) { throw '找不到 go，请先安装 Go 1.26+ 或把它加入 PATH' }

    if (-not $Version) { $Version = (Get-Content (Join-Path $root 'VERSION') -Raw -Encoding ascii).Trim() }
    if (-not (Test-Path (Join-Path $root 'web\dist\index.html'))) {
        throw 'web\dist 不存在。先构建前端：cd web; pnpm install; pnpm build'
    }

    $env:CGO_ENABLED = '0'
    $env:GOFLAGS = '-trimpath'
    $ldflags = "-s -w -X main.version=$Version"

    if (-not $All) {
        & $go build -trimpath -ldflags $ldflags -o 'reelkeep.exe' ./cmd/reelkeep
        if ($LASTEXITCODE -ne 0) { throw "构建失败 ($LASTEXITCODE)" }
        Write-Host "已生成 reelkeep.exe（$Version）" -ForegroundColor Green
        return
    }

    $targets = @(
        @{ os = 'windows'; arch = 'amd64'; ext = '.exe' },
        @{ os = 'darwin';  arch = 'amd64'; ext = '' },
        @{ os = 'darwin';  arch = 'arm64'; ext = '' },
        @{ os = 'linux';   arch = 'amd64'; ext = '' },
        @{ os = 'linux';   arch = 'arm64'; ext = '' }
    )
    $outDir = Join-Path $root 'dist'
    if (-not (Test-Path $outDir)) { New-Item -ItemType Directory -Path $outDir | Out-Null }
    foreach ($t in $targets) {
        $name = "reelkeep-$Version-$($t.os)-$($t.arch)$($t.ext)"
        $env:GOOS = $t.os
        $env:GOARCH = $t.arch
        Write-Host "构建 $name …" -ForegroundColor Cyan
        & $go build -trimpath -ldflags $ldflags -o (Join-Path $outDir $name) ./cmd/reelkeep
        if ($LASTEXITCODE -ne 0) { throw "构建失败: $name" }
    }
    Get-ChildItem $outDir -File | ForEach-Object {
        "{0}  {1:N1} MB" -f $_.Name, ($_.Length / 1MB)
    }
}
finally {
    Pop-Location
}
