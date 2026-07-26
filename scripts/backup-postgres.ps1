param(
    [Parameter(Mandatory = $true)][string]$Container,
    [Parameter(Mandatory = $true)][string]$Database,
    [Parameter(Mandatory = $true)][string]$User,
    [string]$OutputDirectory = (Join-Path (Split-Path -Parent $PSScriptRoot) 'backups')
)
$ErrorActionPreference = 'Stop'
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw 'Docker is required for this backup command.' }
$resolvedOutput = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $resolvedOutput | Out-Null
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$target = Join-Path $resolvedOutput ("$Database-$stamp.dump")
$containerFile = "/tmp/$Database-$stamp.dump"
docker exec $Container pg_dump -U $User -d $Database -Fc -f $containerFile
docker cp "${Container}:$containerFile" $target
if ((Get-Item -LiteralPath $target).Length -eq 0) { throw 'Backup file is empty.' }
Write-Host "Backup created: $target"
