param(
    [string]$Version = '1.0.0-rc.1',
    [string]$Commit = 'development',
    [string]$BuildDate = 'unknown'
)
$ErrorActionPreference = 'Stop'
if ($Version -notmatch '^[A-Za-z0-9._-]+$' -or $Commit -notmatch '^[A-Za-z0-9._-]+$' -or $BuildDate -notmatch '^[A-Za-z0-9:._+-]+$') { throw 'Invalid build metadata' }
$projectRoot = Split-Path $PSScriptRoot -Parent
Push-Location $projectRoot
$savedOS, $savedArch, $savedCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
try {
    go mod download all
    if ($LASTEXITCODE) { throw 'Module download failed' }
    go mod verify
    if ($LASTEXITCODE) { throw 'Module verification failed' }
    $dist = Join-Path $projectRoot 'dist'
    New-Item -ItemType Directory -Force $dist | Out-Null
    $dependencyText = go list -m all
    if ($LASTEXITCODE) { throw 'Dependency inventory failed' }
    $dependencyText | Set-Content -Encoding utf8 (Join-Path $dist 'dependencies.txt')
    $licenseDir = Join-Path $dist 'third-party-licenses'
    New-Item -ItemType Directory -Force $licenseDir | Out-Null
    $goRoot = go env GOROOT
    Copy-Item -LiteralPath (Join-Path $goRoot 'LICENSE') -Destination (Join-Path $licenseDir 'Go-LICENSE') -Force
    $moduleLines = go list -m '-f={{.Path}}|{{.Version}}|{{.Dir}}' all
    if ($LASTEXITCODE) { throw 'Module paths unavailable' }
    foreach ($line in $moduleLines) {
        $parts = $line -split '\|', 3
        if ($parts.Count -ne 3 -or -not $parts[1]) { continue }
        $name = ($parts[0] -replace '[/\\]', '_') + '@' + $parts[1]
        $destination = Join-Path $licenseDir $name
        New-Item -ItemType Directory -Force $destination | Out-Null
        $licenses = Get-ChildItem -LiteralPath $parts[2] -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE)' }
        if (-not $licenses) { throw "No top-level license found for $name; review manually" }
        foreach ($license in $licenses) { Copy-Item -LiteralPath $license.FullName -Destination $destination -Force }
    }
    $targets = @('windows/amd64','linux/amd64','linux/arm64','darwin/amd64','darwin/arm64')
    foreach ($target in $targets) {
        $env:GOOS, $env:GOARCH = $target -split '/'
        $env:CGO_ENABLED = '0'
        $folder = Join-Path $dist ("privsan-$Version-" + ($target -replace '/', '-'))
        if (Test-Path -LiteralPath $folder) { throw "Output already exists: $folder; use a fresh release version or review old output" }
        New-Item -ItemType Directory $folder | Out-Null
        $binary = if ($env:GOOS -eq 'windows') { 'privsan.exe' } else { 'privsan' }
        $flags = "-s -w -buildid= -X privsan/internal/model.Version=$Version -X privsan/internal/model.Commit=$Commit -X privsan/internal/model.BuildDate=$BuildDate"
        go build -trimpath -buildvcs=false -ldflags $flags -o (Join-Path $folder $binary) .
        if ($LASTEXITCODE) { throw "Build failed: $target" }
        Copy-Item -LiteralPath README.md,README.zh-CN.md,LICENSE,NOTICE.md,SECURITY.md,CONTRIBUTING.md,CHANGELOG.md,privsan.example.json -Destination $folder
        # Curate public docs explicitly: ignored local specs must never be packaged.
        $publicDocs = @('CLI.md','TUI.md','OPERATIONS.md','policy.schema.json','report.schema.json')
        $publicDocsDir = Join-Path $folder 'docs'
        New-Item -ItemType Directory $publicDocsDir | Out-Null
        foreach ($document in $publicDocs) {
            Copy-Item -LiteralPath (Join-Path 'docs' $document) -Destination $publicDocsDir
        }
        Copy-Item -LiteralPath examples,$licenseDir -Destination $folder -Recurse
        Copy-Item -LiteralPath (Join-Path $dist 'dependencies.txt') -Destination $folder
        [IO.Compression.ZipFile]::CreateFromDirectory($folder, ($folder + '.zip'), [IO.Compression.CompressionLevel]::Optimal, $true)
    }
    $checksums = Get-ChildItem -LiteralPath $dist -Filter '*.zip' -File | Sort-Object Name | ForEach-Object {
        $hash = Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256
        $hash.Hash.ToLowerInvariant() + '  ' + $_.Name
    }
    $checksums | Set-Content -Encoding ascii (Join-Path $dist 'SHA256SUMS')
} finally {
    $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $savedOS, $savedArch, $savedCGO
    Pop-Location
}
