# Regenerate sources/index.json from the packages in sources/.
# Usage: .\scripts\index-sources.ps1
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$dir = Join-Path $root 'sources'

$entries = @()
foreach ($file in Get-ChildItem $dir -Filter '*.json' | Sort-Object Name) {
    if ($file.Name -eq 'index.json') { continue }
    $pkg = Get-Content $file.FullName -Raw -Encoding UTF8 | ConvertFrom-Json
    if (-not $pkg.id -or -not $pkg.version) { throw "missing id/version: $($file.Name)" }
    $hash = (Get-FileHash $file.FullName -Algorithm SHA256).Hash.ToLower()
    $entries += [ordered]@{
        id          = $pkg.id
        name        = $pkg.name
        version     = $pkg.version
        file        = $file.Name
        sha256      = $hash
        description = $pkg.description
    }
}

$index = [ordered]@{ version = 1; sources = $entries }
$out = Join-Path $dir 'index.json'
$json = $index | ConvertTo-Json -Depth 5
[System.IO.File]::WriteAllText($out, $json + "`n", [System.Text.UTF8Encoding]::new($false))
Write-Host "wrote $out ($($entries.Count) sources)"
