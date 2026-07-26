param(
    [Parameter(Mandatory = $true)][string]$Container,
    [Parameter(Mandatory = $true)][string]$Database,
    [Parameter(Mandatory = $true)][string]$User,
    [Parameter(Mandatory = $true)][string]$BackupFile
)
$ErrorActionPreference = 'Stop'
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw 'Docker is required for this restore command.' }
$resolvedBackup = [IO.Path]::GetFullPath($BackupFile)
if (-not (Test-Path -LiteralPath $resolvedBackup -PathType Leaf)) { throw "Backup not found: $resolvedBackup" }
docker exec $Container createdb -U $User $Database
$containerFile = "/tmp/napkey-restore-$([guid]::NewGuid().ToString('N')).dump"
docker cp $resolvedBackup "${Container}:$containerFile"
docker exec $Container pg_restore -U $User -d $Database --exit-on-error $containerFile
docker exec $Container psql -U $User -d $Database -v ON_ERROR_STOP=1 -c 'SELECT count(*) FROM schema_migrations; SELECT count(*) FROM ledger_entries;'
Write-Host "Restore verification passed for database: $Database"
