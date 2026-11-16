param(
    [Parameter(Mandatory = $true)][string]$Path
)
# Install a validated source package into %APPDATA%\Reelkeep\sources. ASCII-only.
$ErrorActionPreference = 'Stop'
$exe = (Get-Command reelkeep -ErrorAction SilentlyContinue).Source
if (-not $exe) {
    $candidates = @(
        (Join-Path $env:REELKEEP_HOME 'reelkeep.exe'),
        'D:\project\reelkeep\reelkeep.exe',
        (Join-Path $PSScriptRoot '..\..\..\reelkeep.exe')
    )
    foreach ($p in $candidates) {
        if ($p -and (Test-Path $p)) { $exe = (Resolve-Path $p).Path; break }
    }
}
if (-not $exe) {
    throw 'reelkeep.exe not found. Set REELKEEP_HOME or add it to PATH.'
}

& $exe source validate $Path
if ($LASTEXITCODE -ne 0) {
    throw 'validation failed; fix the errors above before installing'
}

$pkg = Get-Content -LiteralPath $Path -Raw -Encoding UTF8 | ConvertFrom-Json
if (-not $pkg.id) { throw 'source package has no id' }

$dir = Join-Path $env:APPDATA 'Reelkeep\sources'
New-Item -ItemType Directory -Path $dir -Force | Out-Null
$dest = Join-Path $dir ($pkg.id + '.json')
Copy-Item -LiteralPath $Path -Destination $dest -Force
Write-Host "installed: $dest"
Write-Host 'restart Reelkeep to load the new source'
