param(
    [ValidateSet("rest", "worker", "both")]
    [string]$Mode = "both",
    [int]$Samples = 20,
    [int]$Concurrency = 4,
    [string]$Benchtime = "10x",
    [string]$OutDir = "",
    [switch]$IncludeWrites,
    [switch]$RunGoBench,
    [switch]$SkipTests,
    [switch]$Full
)

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if ([string]::IsNullOrWhiteSpace($OutDir)) {
    $OutDir = Join-Path $repoRoot "runs"
}
if ($Samples -le 0 -or $Concurrency -le 0) {
    throw "Samples and Concurrency must be positive"
}
if ($Full) {
    $IncludeWrites = $true
    $RunGoBench = $true
    $SkipTests = $false
}

function Get-FirstEnv {
    param([string[]]$Names)
    foreach ($name in $Names) {
        $value = [Environment]::GetEnvironmentVariable($name)
        if (![string]::IsNullOrWhiteSpace($value)) {
            return $value
        }
    }
    return ""
}

function Get-MissingModeEnv {
    param([string]$ExecutorMode)
    if ($ExecutorMode -eq "rest") {
        $missing = @()
        if ([string]::IsNullOrWhiteSpace((Get-FirstEnv @("D1_ACCOUNT_ID", "ACCOUNT_ID")))) { $missing += "D1_ACCOUNT_ID" }
        if ([string]::IsNullOrWhiteSpace((Get-FirstEnv @("D1_API_TOKEN", "API_TOKEN")))) { $missing += "D1_API_TOKEN" }
        if ([string]::IsNullOrWhiteSpace((Get-FirstEnv @("D1_DATABASE_ID", "DATABASE_ID")))) { $missing += "D1_DATABASE_ID" }
        return $missing
    }
    $missing = @()
    if ([string]::IsNullOrWhiteSpace($env:D1_WORKER_ENDPOINT)) { $missing += "D1_WORKER_ENDPOINT" }
    if ([string]::IsNullOrWhiteSpace($env:D1_WORKER_TOKEN)) { $missing += "D1_WORKER_TOKEN" }
    return $missing
}

function Invoke-Captured {
    param(
        [scriptblock]$Command,
        [string]$OutputPath
    )
    $output = & $Command 2>&1
    $exitCode = $LASTEXITCODE
    $output | Out-String | Set-Content -LiteralPath $OutputPath -Encoding UTF8
    return $exitCode
}

function Add-OperationTable {
    param(
        [System.Text.StringBuilder]$Builder,
        [object]$Report
    )
    [void]$Builder.AppendLine("## $($Report.mode.ToString().ToUpperInvariant())")
    [void]$Builder.AppendLine("")
    [void]$Builder.AppendLine("| Operation | p50 ms | p95 ms | p99 ms | Mean ms | Failures | Rows read | Rows written |")
    [void]$Builder.AppendLine("|---|---:|---:|---:|---:|---:|---:|---:|")
    foreach ($operation in $Report.operations) {
        $failures = "$($operation.failed)/$($operation.attempted)"
        [void]$Builder.AppendLine(("| {0} | {1:N2} | {2:N2} | {3:N2} | {4:N2} | {5} | {6} | {7} |" -f `
            $operation.name, $operation.p50Ms, $operation.p95Ms, $operation.p99Ms, $operation.meanMs, $failures, `
            $operation.d1Meta.rowsRead, $operation.d1Meta.rowsWritten))
    }
    [void]$Builder.AppendLine("")
}

$runStamp = Get-Date -Format "yyyyMMdd-HHmmss"
$runDir = Join-Path $OutDir "run-$runStamp"
New-Item -ItemType Directory -Force -Path $runDir | Out-Null

$requestedModes = if ($Mode -eq "both") { @("rest", "worker") } else { @($Mode) }
$readyModes = @()
$skippedModes = @()
foreach ($executorMode in $requestedModes) {
    $missing = @(Get-MissingModeEnv $executorMode)
    if ($missing.Count -eq 0) {
        $readyModes += $executorMode
    }
    else {
        $skippedModes += [ordered]@{ mode = $executorMode; missing = $missing }
    }
}

$failed = $false
$oldMode = $env:D1_EXECUTOR_MODE
$oldWrites = $env:D1_INCLUDE_WRITES
try {
    if (!$SkipTests) {
        $workerDir = Join-Path $repoRoot "workers\d1-proxy"
        if (!(Test-Path -LiteralPath (Join-Path $workerDir "node_modules"))) {
            Push-Location $workerDir
            try {
                $npmInstallCode = Invoke-Captured { npm ci } (Join-Path $runDir "worker-npm-ci.txt")
                if ($npmInstallCode -ne 0) { $failed = $true }
            }
            finally {
                Pop-Location
            }
        }
        Push-Location $workerDir
        try {
            $workerCheckCode = Invoke-Captured { npm run check } (Join-Path $runDir "worker-check.txt")
            $workerTestCode = Invoke-Captured { npm test } (Join-Path $runDir "worker-test.txt")
            $workerBundleCode = Invoke-Captured { npx wrangler deploy --dry-run --outdir dist } (Join-Path $runDir "worker-bundle.txt")
            if ($workerCheckCode -ne 0 -or $workerTestCode -ne 0 -or $workerBundleCode -ne 0) { $failed = $true }
        }
        finally {
            Pop-Location
        }
    }

    $reports = @()
    foreach ($executorMode in $readyModes) {
        $env:D1_EXECUTOR_MODE = $executorMode
        $env:D1_INCLUDE_WRITES = if ($IncludeWrites) { "1" } else { "" }

        if (!$SkipTests) {
            Push-Location $repoRoot
            try {
                $testPath = Join-Path $runDir "$executorMode-go-test.txt"
                $testCode = Invoke-Captured { go test ./... -count 1 } $testPath
                if ($testCode -ne 0) { $failed = $true }
            }
            finally {
                Pop-Location
            }
        }

        $jsonPath = Join-Path $runDir "$executorMode.json"
        $logPath = Join-Path $runDir "$executorMode-d1bench.txt"
        Push-Location $repoRoot
        try {
            if ($IncludeWrites) {
                $benchCode = Invoke-Captured { go run ./cmd/d1bench -mode $executorMode -samples $Samples -concurrency $Concurrency -writes -out $jsonPath } $logPath
            }
            else {
                $benchCode = Invoke-Captured { go run ./cmd/d1bench -mode $executorMode -samples $Samples -concurrency $Concurrency -out $jsonPath } $logPath
            }
            if ($benchCode -ne 0) { $failed = $true }

            if ($RunGoBench) {
                $goBenchPath = Join-Path $runDir "$executorMode-go-bench.txt"
                $goBenchCode = Invoke-Captured { go test -run '^$' -bench . -benchmem -benchtime $Benchtime } $goBenchPath
                if ($goBenchCode -ne 0) { $failed = $true }
            }
        }
        finally {
            Pop-Location
        }
        if (Test-Path -LiteralPath $jsonPath) {
            $reports += Get-Content -LiteralPath $jsonPath -Raw | ConvertFrom-Json
        }
    }
}
finally {
    $env:D1_EXECUTOR_MODE = $oldMode
    $env:D1_INCLUDE_WRITES = $oldWrites
}

$status = if ($failed) {
    "failed"
}
elseif ($reports.Count -eq 0) {
    "blocked"
}
elseif ($skippedModes.Count -gt 0) {
    "partial"
}
else {
    "complete"
}

$summary = [ordered]@{
    status = $status
    createdAt = (Get-Date).ToString("o")
    samples = $Samples
    concurrency = $Concurrency
    includeWrites = [bool]$IncludeWrites
    runGoBench = [bool]$RunGoBench
    reports = $reports
    skippedModes = $skippedModes
}
$summaryJsonPath = Join-Path $runDir "summary.json"
$summary | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $summaryJsonPath -Encoding UTF8

$builder = [System.Text.StringBuilder]::new()
[void]$builder.AppendLine("# D1 Adapter Benchmark")
[void]$builder.AppendLine("")
[void]$builder.AppendLine("Status: $status")
[void]$builder.AppendLine("")
[void]$builder.AppendLine("Samples per operation: $Samples")
[void]$builder.AppendLine("")
[void]$builder.AppendLine("Concurrency: $Concurrency")
[void]$builder.AppendLine("")
[void]$builder.AppendLine("Include writes: $([bool]$IncludeWrites)")
[void]$builder.AppendLine("")
foreach ($report in $reports) {
    Add-OperationTable $builder $report
}

$restReport = $reports | Where-Object { $_.mode -eq "rest" } | Select-Object -First 1
$workerReport = $reports | Where-Object { $_.mode -eq "worker" } | Select-Object -First 1
if ($null -ne $restReport -and $null -ne $workerReport) {
    [void]$builder.AppendLine("## REST vs Worker")
    [void]$builder.AppendLine("")
    [void]$builder.AppendLine("A ratio above 1.00x means Worker is faster.")
    [void]$builder.AppendLine("")
    [void]$builder.AppendLine("| Operation | REST p50 ms | Worker p50 ms | p50 ratio | REST p95 ms | Worker p95 ms | p95 ratio |")
    [void]$builder.AppendLine("|---|---:|---:|---:|---:|---:|---:|")
    foreach ($restOperation in $restReport.operations) {
        $workerOperation = $workerReport.operations | Where-Object { $_.name -eq $restOperation.name } | Select-Object -First 1
        if ($null -eq $workerOperation) { continue }
        $p50Ratio = if ($workerOperation.p50Ms -gt 0) { $restOperation.p50Ms / $workerOperation.p50Ms } else { 0 }
        $p95Ratio = if ($workerOperation.p95Ms -gt 0) { $restOperation.p95Ms / $workerOperation.p95Ms } else { 0 }
        [void]$builder.AppendLine(("| {0} | {1:N2} | {2:N2} | {3:N2}x | {4:N2} | {5:N2} | {6:N2}x |" -f `
            $restOperation.name, $restOperation.p50Ms, $workerOperation.p50Ms, $p50Ratio, `
            $restOperation.p95Ms, $workerOperation.p95Ms, $p95Ratio))
    }
    [void]$builder.AppendLine("")
}

if ($skippedModes.Count -gt 0) {
    [void]$builder.AppendLine("## Skipped")
    [void]$builder.AppendLine("")
    foreach ($skipped in $skippedModes) {
        [void]$builder.AppendLine("- $($skipped.mode): missing $($skipped.missing -join ', ')")
    }
    [void]$builder.AppendLine("")
}

$summaryMarkdownPath = Join-Path $runDir "summary.md"
$builder.ToString() | Set-Content -LiteralPath $summaryMarkdownPath -Encoding UTF8
Write-Host "Wrote $summaryMarkdownPath"
Write-Host "Wrote $summaryJsonPath"

if ($status -eq "failed") { exit 1 }
if ($status -eq "blocked") { exit 2 }
exit 0
