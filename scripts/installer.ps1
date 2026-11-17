param(
    [string]$Version = '',
    [switch]$SkipBuild
)

$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    if (-not $Version) {
        $Version = (Get-Content (Join-Path $root 'VERSION') -Raw).Trim()
    }
    if (-not $SkipBuild) {
        & (Join-Path $PSScriptRoot 'build.ps1')
        if ($LASTEXITCODE -ne 0) { throw "build.ps1 failed" }
    }

    $exe = Join-Path $root 'reelkeep-desktop.exe'
    if (-not (Test-Path $exe)) { throw "missing $exe; run scripts\build.ps1 first" }
    $dist = Join-Path $root 'dist'
    if (-not (Test-Path $dist)) { New-Item -ItemType Directory -Path $dist | Out-Null }

    # Bundle WebView2 bootstrapper when available.
    $webview = Join-Path $root 'build\MicrosoftEdgeWebview2Setup.exe'
    if (-not (Test-Path $webview)) {
        try {
            New-Item -ItemType Directory -Force (Split-Path $webview) | Out-Null
            Invoke-WebRequest -UseBasicParsing -Uri 'https://go.microsoft.com/fwlink/p/?LinkId=2124703' -OutFile $webview -TimeoutSec 90
            Write-Host "downloaded WebView2 bootstrapper" -ForegroundColor Green
        } catch {
            Write-Warning "WebView2 bootstrapper download failed: $($_.Exception.Message)"
        }
    }

    $makensis = 'C:\Program Files (x86)\NSIS\makensis.exe'
    if (Get-Command makensis -ErrorAction SilentlyContinue) {
        $makensis = (Get-Command makensis).Source
    }
    if (-not (Test-Path $makensis)) { throw "makensis not found; install NSIS (winget install NSIS.NSIS)" }

    & $makensis "/DVERSION=$Version" (Join-Path $root 'installer\reelkeep.nsi')
    if ($LASTEXITCODE -ne 0) { throw "makensis failed ($LASTEXITCODE)" }

    $setup = Join-Path $dist "Reelkeep-Setup-$Version.exe"
    if (-not (Test-Path $setup)) { throw "installer not produced: $setup" }
    $mb = [math]::Round((Get-Item $setup).Length / 1MB, 2)
    Write-Host "built $setup ($mb MB)" -ForegroundColor Green
}
finally {
    Pop-Location
}
