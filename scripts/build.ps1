# Build script: produce a single binary with web/dist embedded.
#
# Usage:
#   .\scripts\build.ps1                    # local Windows build
#   .\scripts\build.ps1 -All               # cross-compile 5 targets into dist\
#   .\scripts\build.ps1 -Version 0.2.0     # override version
#
# Keep this file ASCII-only: Windows PowerShell 5.1 reads BOM-less UTF-8 as ANSI.
param(
    [string]$Version = '',
    [switch]$All
)

$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    $go = if ($cmd) { $cmd.Source } else { '' }
    if (-not $go) {
        foreach ($p in @('D:\software\go\bin\go.exe', "$env:ProgramFiles\Go\bin\go.exe", "$env:LOCALAPPDATA\Programs\Go\bin\go.exe")) {
            if (Test-Path $p) { $go = $p; break }
        }
    }
    if (-not $go) { throw 'go not found; install Go 1.26+ or add it to PATH' }

    if (-not $Version) { $Version = (Get-Content (Join-Path $root 'VERSION') -Raw -Encoding ascii).Trim() }
    if (-not (Test-Path (Join-Path $root 'web\dist\index.html'))) {
        throw 'web\dist is missing. Build the frontend first: cd web; pnpm install; pnpm build'
    }

    $env:CGO_ENABLED = '0'
    $ldflags = "-s -w -X main.version=$Version"

    if (-not $All) {
        & $go build -trimpath -ldflags $ldflags -o 'reelkeep.exe' ./cmd/reelkeep
        if ($LASTEXITCODE -ne 0) { throw "build failed ($LASTEXITCODE)" }
        $mb = [math]::Round((Get-Item 'reelkeep.exe').Length / 1MB, 2)
        Write-Host "built reelkeep.exe ($Version, $mb MB)" -ForegroundColor Green
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
        Write-Host "building $name ..." -ForegroundColor Cyan
        & $go build -trimpath -ldflags $ldflags -o (Join-Path $outDir $name) ./cmd/reelkeep
        if ($LASTEXITCODE -ne 0) { throw "build failed: $name" }
    }
    Get-ChildItem $outDir -File | ForEach-Object {
        "{0}  {1:N1} MB" -f $_.Name, ($_.Length / 1MB)
    }
}
finally {
    Pop-Location
}
