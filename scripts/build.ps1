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

    function New-SourceZip {
        $dist = Join-Path $root 'dist'
        if (-not (Test-Path $dist)) { New-Item -ItemType Directory -Path $dist | Out-Null }
        $tmp = Join-Path $env:TEMP ('reelkeep-sources-' + [guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $tmp | Out-Null
        try {
            Copy-Item (Join-Path $root 'sources\*.json') $tmp -Force
            $zip = Join-Path $dist "reelkeep-sources-$Version.zip"
            Compress-Archive -Path (Join-Path $tmp '*') -DestinationPath $zip -Force
            Write-Host "packaged $zip" -ForegroundColor Green
        } finally {
            Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
        }
    }

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

        # Desktop shell (Wails v3 + WebView2, CGO disabled)
        & $go build -trimpath -ldflags "-H windowsgui $ldflags" -o 'reelkeep-desktop.exe' ./desktop
        if ($LASTEXITCODE -ne 0) { throw "desktop build failed ($LASTEXITCODE)" }
        $dmb = [math]::Round((Get-Item 'reelkeep-desktop.exe').Length / 1MB, 2)
        Write-Host "built reelkeep-desktop.exe ($Version, $dmb MB)" -ForegroundColor Green
        New-SourceZip
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

    # Desktop shell is Windows-only for now (WebView2)
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $deskName = "reelkeep-$Version-windows-amd64-desktop.exe"
    Write-Host "building $deskName ..." -ForegroundColor Cyan
    & $go build -trimpath -ldflags "-H windowsgui $ldflags" -o (Join-Path $outDir $deskName) ./desktop
    if ($LASTEXITCODE -ne 0) { throw "desktop build failed" }

    New-SourceZip
    Get-ChildItem $outDir -File | ForEach-Object {
        "{0}  {1:N1} MB" -f $_.Name, ($_.Length / 1MB)
    }
}
finally {
    Pop-Location
}
