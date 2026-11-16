param(
    [Parameter(Mandatory = $true)][string]$Path
)
# Validate a Reelkeep source package. Keep this file ASCII-only.
$ErrorActionPreference = 'Stop'
$exe = (Get-Command reelkeep -ErrorAction SilentlyContinue).Source
if (-not $exe) {
    $candidates = @()
    if ($env:REELKEEP_HOME) { $candidates += (Join-Path $env:REELKEEP_HOME 'reelkeep.exe') }
    $candidates += 'D:\project\reelkeep\reelkeep.exe'
    $candidates += (Join-Path $PSScriptRoot '..\..\..\reelkeep.exe')
    foreach ($p in $candidates) {
        if ($p -and (Test-Path $p)) { $exe = (Resolve-Path $p).Path; break }
    }
}
if (-not $exe) {
    throw 'reelkeep.exe not found. Set REELKEEP_HOME or add it to PATH.'
}
& $exe source validate $Path
exit $LASTEXITCODE
