$ErrorActionPreference = 'Stop'
$taskSource = Join-Path $PSScriptRoot 'dispatcher'
$taskManifest = Get-Content (Join-Path $taskSource 'manifest.json') -Raw | ConvertFrom-Json
$taskOutput = Join-Path $PSScriptRoot 'dist'
New-Item -ItemType Directory -Force -Path $taskOutput | Out-Null
$taskZip = Join-Path $taskOutput ("dispatch-watch-" + $taskManifest.version + '.zip')
Compress-Archive -Path (Join-Path $taskSource '*') -DestinationPath $taskZip -Force
Write-Output $taskZip
