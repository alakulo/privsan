$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
Push-Location $projectRoot
try {
    $run = Join-Path $projectRoot ('build/smoke-' + [guid]::NewGuid().ToString('N'))
    $source = Join-Path $run 'source'
    New-Item -ItemType Directory -Force $source | Out-Null
    $binary = Join-Path $run 'privsan.exe'
    go build -trimpath -o $binary .
    if ($LASTEXITCODE) { throw 'Build failed' }
    $inputPath = Join-Path $source 'sample.txt'
    [IO.File]::WriteAllText($inputPath, "email=alice@example.com`r`nphone=13800138000`r`n")
    $originalHash = (Get-FileHash -LiteralPath $inputPath).Hash
    $report = & $binary scan --json --content --hide-paths $source | ConvertFrom-Json
    if ($LASTEXITCODE -or -not $report.complete -or $report.summary.findings -ne 2) { throw 'Agent scan failed' }
    if ($report.files[0].content -match 'alice@example.com|13800138000') { throw 'Plaintext leak' }
    if ((Get-FileHash -LiteralPath $inputPath).Hash -ne $originalHash) { throw 'Scan changed original' }
    $output = Join-Path $run 'export'
    & $binary redact --output $output $source
    if ($LASTEXITCODE -or -not (Test-Path -LiteralPath (Join-Path $output '.privsan-export.json'))) { throw 'Export failed' }
    $backup = Join-Path $run 'backups'
    $written = & $binary redact --in-place --backup-dir $backup --json $source | ConvertFrom-Json
    if ($LASTEXITCODE -or -not $written.complete) { throw 'In-place write failed' }
    & $binary restore --from $written.operation.run_dir --root $source
    if ($LASTEXITCODE -or (Get-FileHash -LiteralPath $inputPath).Hash -ne $originalHash) { throw 'Restore failed' }
    Write-Output "PASS: scan, JSON, export, in-place, restore ($run)"
} finally { Pop-Location }
