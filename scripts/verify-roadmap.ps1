$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$goCache = Join-Path $env:TEMP 'napkey-go-cache-roadmap'
$env:GOCACHE = $goCache

Push-Location (Join-Path $root 'backend\napkey-core')
try {
    go test ./... -count=1
    go vet ./...
    go build ./...
} finally { Pop-Location }

Push-Location (Join-Path $root 'backend\kiro-go')
try {
    go test ./... -count=1
    go vet ./...
    go build ./...
} finally { Pop-Location }

Push-Location (Join-Path $root 'napkey-web')
try {
    npx.cmd tsc --noEmit --incremental false
    npm.cmd run lint
    npm.cmd audit --offline
	$env:NEXT_DIST_DIR = '.next-roadmap-build'
    npm.cmd run build
} finally { Pop-Location }

Write-Host 'Roadmap verification passed.' -ForegroundColor Green
