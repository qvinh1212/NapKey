#Requires -Version 5.1
<#
.SYNOPSIS
Measures the real upstream cost of each model NapKey sells.

.DESCRIPTION
The price book carries one cost basis -- 2,097 VND/1M tokens plus 110 VND/call
(migrations 0018 and 0019) -- and docs/OPERATIONS.md records that it was measured on
Claude traffic only. Every other model on the pool falls through to the '*' row at
that same basis, unverified. This measures what the upstream actually bills, per
model, so the assumption can be checked rather than trusted.

WHAT IS MEASURED

The upstream bills per token and prepends its own prompt before counting, so the
billable size of a request is the caller's text plus a fixed per-model block. Both
parts are recovered from three request sizes built by repeating one paragraph:

    billed(n) = overhead + rate * n          n = paragraph count

    rate     = slope across the sizes (tokens the upstream charges per paragraph)
    overhead = billed(n) - rate * n          (the injected prompt, in tokens)

Solving for both means no client-side tokenizer is needed, which matters because a
tokenizer estimate would land entirely inside the overhead figure. In the slope it
cancels out.

WHY EACH SIZE IS PROBED MORE THAN ONCE

The pool routes one model to more than one backend and their injected prompts differ
in size: an identical "Hi" to claude-sonnet-5 has been billed both 2,547 and 6,484
tokens. A single probe therefore measures whichever backend answered, not the model.
Each size is probed -Repeat times and the mode is taken, naming the backend that
serves most traffic. ModeShare reports how dominant that backend was; a low value
means the model cost genuinely varies per request and the figure is an average.

Output tokens are measured separately on one fixed open-ended prompt with a real
output budget, because verbosity differs sharply between models and output is billed
at the same rate as input.

.PARAMETER Repeat
Probes per size. Below 3 the mode is meaningless; raise it for a model whose
ModeShare comes back low.

.PARAMETER Concurrency
Probes in flight. The upstream takes seconds per call and a full catalog is over a
hundred probes, so a serial run takes hours. Keep this modest: the key is shared with
production traffic and a burst competes with paying customers.

.EXAMPLE
.\measure-model-cost.ps1 -JsonOut .\model-cost.json

.EXAMPLE
.\measure-model-cost.ps1 -Models @('claude-sonnet-5','glm-5') -Repeat 5
#>
[CmdletBinding()]
param(
    [string]$ApiKey = $env:NINEROUTER_API_KEY,
    [string]$BaseUrl = 'https://gateway-admin.viberouter.io.vn/v1',
    [string]$Prefix = 'Viberouter/',
    [string[]]$Models,
    [int]$Repeat = 3,
    [int]$Concurrency = 4,
    [int]$TimeoutSec = 300,
    [int]$MaxAttempts = 5,
    [string]$JsonOut,
    [switch]$IncludeThinking,
    [switch]$SkipOutputProbe
)

$ErrorActionPreference = 'Stop'
if (-not $ApiKey) { throw 'ApiKey is required (pass -ApiKey or set NINEROUTER_API_KEY).' }
if ($Repeat -lt 1) { throw 'Repeat must be at least 1.' }
if ($Concurrency -lt 1 -or $Concurrency -gt 16) { throw 'Concurrency must be between 1 and 16.' }
if ($MaxAttempts -lt 1 -or $MaxAttempts -gt 10) { throw 'MaxAttempts must be between 1 and 10.' }

# Price book basis, from migrations 0018 and 0019. Read-only here: this script reports
# what the current book earns against measured traffic and never writes a price.
$RetailPerMillion   = 12000
$UpstreamPerMillion = 2097
$RetailFeeVnd       = 300
$UpstreamFeeVnd     = 110

# Paragraph counts to bill, spread wide enough that the slope is not dominated by
# rounding on any single size.
$SizesInParagraphs = @(4, 12, 28)

# One paragraph of ordinary prose. Natural text matters: repeated filler such as
# "lorem lorem" compresses far better than anything a customer sends, which would put
# the measured slope below the real rate.
$Paragraph = 'The quarterly report shows steady growth across every regional market, with margins holding near the level management guided to at the start of the year. Operating expenses rose modestly, driven mostly by hiring in support and engineering. '

# Open-ended and identical for every model, so output figures are comparable.
$OutputPrompt = 'Explain what an API gateway does and why a team would put one in front of their services.'
$OutputBudget = 600

# A representative caller prompt, in tokens: roughly one coding-agent step. The cost
# of a request is quoted at this size so models are compared on the same workload.
$ReferenceCallerTokens = 1000

$headers = @{ Authorization = "Bearer $ApiKey"; Accept = "*/*"; "User-Agent" = "curl/8.5.0" }

function Get-PoolModels {
    $response = Invoke-RestMethod -Uri "$BaseUrl/models" -Headers $headers -TimeoutSec 60
    $ids = @()
    foreach ($entry in $response.data) {
        $id = [string]$entry.id
        if (-not $id.StartsWith($Prefix, [StringComparison]::OrdinalIgnoreCase)) { continue }
        $public = $id.Substring($Prefix.Length).Trim()
        # A nested namespace has no public id to map back to, matching
        # publicModelsFromUpstream in kiro-go.
        if (-not $public -or $public.Contains("/")) { continue }
        # "auto" picks a different model per request, so a cost measured for it
        # describes whatever it routed to, not the route itself.
        if ($public -eq "auto") { continue }
        if (-not $IncludeThinking -and $public -match "-thinking$") { continue }
        $ids += $public
    }
    return $ids | Sort-Object -Unique
}

# Runs in a background runspace, so it takes everything it needs as arguments and
# shares no state with the caller.
#
# Retries on 429. The upstream throttles a burst, and a throttled probe is not a
# measurement of anything -- discarding it would silently drop whole models from the
# report, which is how the first full run lost 27 of 33. Backoff is exponential with
# jitter so retries from parallel runspaces do not resynchronise into a second burst.
$ProbeScript = {
    param($BaseUrl, $ApiKey, $UpstreamModel, $Content, $MaxTokens, $TimeoutSec, $Tag, $MaxAttempts)

    $result = [pscustomobject]@{ Tag = $Tag; PromptTokens = $null; CompletionTokens = $null; Error = $null; Attempts = 0 }

    # A fresh runspace does not inherit the caller's TLS setting, and PowerShell 5.1
    # defaults to SSL3/TLS 1.0 -- which the upstream rejects at the handshake, before
    # any HTTP status exists. That surfaces as a statusless "request_failed" on every
    # probe while the same call succeeds in the parent shell.
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $payload = @{
        model      = $UpstreamModel
        messages   = @(@{ role = "user"; content = $Content })
        max_tokens = $MaxTokens
        stream     = $true
    } | ConvertTo-Json -Depth 5 -Compress

    for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
        $result.Attempts = $attempt
        try {
            # Cloudflare fronts the gateway and answers the default PowerShell agent
            # with error 1010 before the request reaches the upstream at all.
            $raw = Invoke-WebRequest -Uri "$BaseUrl/chat/completions" -Method Post `
                -Headers @{ Authorization = "Bearer $ApiKey"; Accept = "*/*"; "User-Agent" = "curl/8.5.0" } `
                -ContentType "application/json" `
                -Body $payload -TimeoutSec $TimeoutSec -UseBasicParsing

            # The endpoint always answers as text/event-stream, whatever stream is set
            # to, and reports usage only on the final chunk. Parsing the body as one
            # JSON object fails outright, so every data: line is scanned and the last
            # usage seen wins.
            $usage = $null
            foreach ($line in ($raw.Content -split "`n")) {
                $line = $line.Trim()
                if (-not $line.StartsWith("data: ") -or $line -eq "data: [DONE]") { continue }
                try { $chunk = $line.Substring(6) | ConvertFrom-Json } catch { continue }
                if ($chunk.usage) { $usage = $chunk.usage }
            }
            if (-not $usage) { $result.Error = "no_usage_in_stream"; return $result }

            $result.PromptTokens = [int]$usage.prompt_tokens
            $result.CompletionTokens = [int]$usage.completion_tokens
            $result.Error = $null
            return $result
        }
        catch {
            $response = $_.Exception.Response
            $status = if ($response) { [int]$response.StatusCode } else { 0 }
            # Keep the underlying message when there is no status: a TLS or DNS failure
            # is a different problem from a refusal, and "request_failed" hides which.
            $result.Error = if ($status -gt 0) { "http_$status" } else { "request_failed: " + $_.Exception.Message }

            # 429 and 5xx are the upstream being busy, which a later attempt can pass.
            # A 4xx of any other kind is a verdict on the request itself, so retrying
            # only spends real money on the same refusal.
            $retryable = ($status -eq 429) -or ($status -ge 500) -or ($status -eq 0)
            if (-not $retryable -or $attempt -eq $MaxAttempts) { return $result }

            $delay = [Math]::Min(60, [Math]::Pow(2, $attempt)) + (Get-Random -Minimum 0.0 -Maximum 2.0)
            Start-Sleep -Seconds $delay
        }
    }
    return $result
}
# Fans the probe list out across a runspace pool and returns every result. Failures
# come back as rows with Error set rather than throwing, because one refused model
# must not discard the measurements already paid for.
function Invoke-ProbeBatch {
    param([object[]]$Probes)

    $pool = [runspacefactory]::CreateRunspacePool(1, $Concurrency)
    $pool.Open()
    $running = @()
    try {
        foreach ($probe in $Probes) {
            $shell = [powershell]::Create()
            $shell.RunspacePool = $pool
            [void]$shell.AddScript($ProbeScript).
                AddArgument($BaseUrl).AddArgument($ApiKey).AddArgument($probe.UpstreamModel).
                AddArgument($probe.Content).AddArgument($probe.MaxTokens).
                AddArgument($TimeoutSec).AddArgument($probe.Tag).AddArgument($MaxAttempts)
            $running += [pscustomobject]@{ Shell = $shell; Handle = $shell.BeginInvoke() }
        }

        $done = 0
        $results = @()
        foreach ($item in $running) {
            $results += $item.Shell.EndInvoke($item.Handle)
            $item.Shell.Dispose()
            $done++
            Write-Progress -Activity "Probing upstream" -Status "$done of $($running.Count) probes" `
                -PercentComplete (100 * $done / $running.Count)
        }
        Write-Progress -Activity "Probing upstream" -Completed
        return $results
    }
    finally { $pool.Dispose() }
}

# The most frequent value, with the share of samples that agreed. The mode rather than
# the mean because samples come from a small set of backends with distinct prompt
# sizes, and averaging two backends reports a number neither of them charges.
function Get-Mode {
    param([int[]]$Values, [int]$Tolerance = 0)
    if (-not $Values -or $Values.Count -eq 0) { return $null }
    $best = $null; $bestCount = 0
    foreach ($candidate in $Values) {
        $count = ($Values | Where-Object { [Math]::Abs($_ - $candidate) -le $Tolerance }).Count
        if ($count -gt $bestCount) { $bestCount = $count; $best = $candidate }
    }
    return [pscustomobject]@{ Value = $best; Share = [Math]::Round($bestCount / [double]$Values.Count, 2) }
}

if (-not $Models -or $Models.Count -eq 0) {
    Write-Host "Reading the pool catalog..." -ForegroundColor Cyan
    $Models = Get-PoolModels
}

$probes = @()
foreach ($model in $Models) {
    foreach ($size in $SizesInParagraphs) {
        $content = "Summarize this. " + ($Paragraph * $size)
        for ($i = 0; $i -lt $Repeat; $i++) {
            $probes += [pscustomobject]@{
                Tag = "$model|size|$size"; UpstreamModel = "$Prefix$model"; Content = $content; MaxTokens = 16
            }
        }
    }
    if (-not $SkipOutputProbe) {
        $probes += [pscustomobject]@{
            Tag = "$model|output|0"; UpstreamModel = "$Prefix$model"; Content = $OutputPrompt; MaxTokens = $OutputBudget
        }
    }
}

Write-Host ("Measuring {0} models with {1} probes, {2} at a time, against {3}" -f `
    $Models.Count, $probes.Count, $Concurrency, $BaseUrl) -ForegroundColor Cyan
Write-Host ""

$raw = Invoke-ProbeBatch -Probes $probes

# Index the flat results back onto model and probe kind.
$byModel = @{}
foreach ($row in $raw) {
    $parts = ([string]$row.Tag).Split("|")
    $model = $parts[0]; $kind = $parts[1]; $size = $parts[2]
    if (-not $byModel.ContainsKey($model)) { $byModel[$model] = @{ Sizes = @{}; Output = @(); Errors = @() } }
    if ($row.Error) { $byModel[$model].Errors += $row.Error; continue }
    if ($kind -eq "size") {
        $key = [int]$size
        if (-not $byModel[$model].Sizes.ContainsKey($key)) { $byModel[$model].Sizes[$key] = @() }
        $byModel[$model].Sizes[$key] += [int]$row.PromptTokens
    }
    else { $byModel[$model].Output += [int]$row.CompletionTokens }
}

$results = @()
foreach ($model in $Models) {
    $bucket = $byModel[$model]
    $measuredSizes = @($SizesInParagraphs | Where-Object { $bucket -and $bucket.Sizes.ContainsKey($_) -and $bucket.Sizes[$_].Count -gt 0 })

    if ($measuredSizes.Count -lt 2) {
        $status = if ($bucket -and $bucket.Errors.Count -gt 0) { ($bucket.Errors | Group-Object | Sort-Object Count -Descending | Select-Object -First 1).Name } else { "no_data" }
        $results += [pscustomobject]@{
            Model = $model; OverheadTokens = $null; TokPerPara = $null; Linearity = $null
            ModeShare = $null; OutputTokens = $null; BilledTokens = $null; UpstreamVnd = $null
            RetailVnd = $null; MarginPct = $null; Status = $status
        }
        continue
    }

    $billed = @{}
    $shares = @()
    foreach ($size in $measuredSizes) {
        # Tolerance absorbs the few-token jitter some models show on identical input
        # while still separating backends that differ by thousands.
        $mode = Get-Mode -Values $bucket.Sizes[$size] -Tolerance 32
        $billed[$size] = $mode.Value
        $shares += $mode.Share
    }

    # Slope across the widest measured pair, which carries the least rounding error.
    $first = $measuredSizes[0]
    $last = $measuredSizes[-1]
    $rate = ($billed[$last] - $billed[$first]) / [double]($last - $first)
    $overhead = [int][Math]::Round($billed[$first] - $rate * $first)

    # Linearity check on the middle point. A model that is not linear in caller text is
    # not billed the way this basis assumes, so it is flagged rather than averaged over.
    $linearity = $null
    if ($measuredSizes.Count -ge 3) {
        $mid = $measuredSizes[1]
        $predicted = $overhead + $rate * $mid
        if ($billed[$mid] -gt 0) { $linearity = [Math]::Round([Math]::Abs($billed[$mid] - $predicted) / [double]$billed[$mid], 3) }
    }

    $outputTokens = if ($bucket.Output.Count -gt 0) { [int](($bucket.Output | Measure-Object -Average).Average) } else { $null }

    $billableTotal = $overhead + $ReferenceCallerTokens + [int]$outputTokens
    $upstreamVnd = [Math]::Round(($billableTotal * $UpstreamPerMillion / 1000000.0) + $UpstreamFeeVnd, 2)
    $retailVnd = [Math]::Round(($billableTotal * $RetailPerMillion / 1000000.0) + $RetailFeeVnd, 2)
    $marginPct = if ($retailVnd -gt 0) { [Math]::Round(100.0 * ($retailVnd - $upstreamVnd) / $retailVnd, 1) } else { $null }

    $results += [pscustomobject]@{
        Model          = $model
        OverheadTokens = $overhead
        TokPerPara     = [Math]::Round($rate, 1)
        Linearity      = $linearity
        ModeShare      = [Math]::Round(($shares | Measure-Object -Average).Average, 2)
        OutputTokens   = $outputTokens
        BilledTokens   = $billableTotal
        UpstreamVnd    = $upstreamVnd
        RetailVnd      = $retailVnd
        MarginPct      = $marginPct
        Status         = if ($bucket.Errors.Count -gt 0) { "partial" } else { "ok" }
    }
}

# Measured rows first and cheapest-first within them, so the table opens on the
# comparison it exists for. Unmeasured models sort to the bottom carrying their
# reason, rather than appearing as blank rows that look like a rendering fault.
$results |
    Sort-Object @{ Expression = { $null -eq $_.UpstreamVnd }; Ascending = $true }, UpstreamVnd |
    Format-Table -AutoSize -Property Model, OverheadTokens, TokPerPara, Linearity, ModeShare,
        OutputTokens, BilledTokens, UpstreamVnd, RetailVnd, MarginPct, Status

$ok = $results | Where-Object { $_.Status -in @("ok", "partial") }
if ($ok) {
    Write-Host "Summary" -ForegroundColor Cyan
    $low = $ok | Sort-Object UpstreamVnd | Select-Object -First 1
    $high = $ok | Sort-Object UpstreamVnd -Descending | Select-Object -First 1
    Write-Host ("  cheapest request : {0} at {1} VND ({2} overhead tokens)" -f $low.Model, $low.UpstreamVnd, $low.OverheadTokens)
    Write-Host ("  dearest request  : {0} at {1} VND ({2} overhead tokens)" -f $high.Model, $high.UpstreamVnd, $high.OverheadTokens)
    Write-Host ("  margin range     : {0}% to {1}%" -f `
        ($ok | Measure-Object -Property MarginPct -Minimum).Minimum, ($ok | Measure-Object -Property MarginPct -Maximum).Maximum)
    Write-Host ("  overhead range   : {0} to {1} tokens per request" -f `
        ($ok | Measure-Object -Property OverheadTokens -Minimum).Minimum, ($ok | Measure-Object -Property OverheadTokens -Maximum).Maximum)

    $unstable = $ok | Where-Object { $_.ModeShare -ne $null -and $_.ModeShare -lt 0.6 }
    if ($unstable) {
        Write-Host ""
        Write-Host "  Cost varies per request. The pool answered these from backends with different" -ForegroundColor Yellow
        Write-Host "  injected prompts, so the figure is an average rather than a rate:" -ForegroundColor Yellow
        foreach ($row in $unstable) { Write-Host ("    {0}: mode agreed on {1:P0} of probes" -f $row.Model, $row.ModeShare) -ForegroundColor Yellow }
    }

    $nonlinear = $ok | Where-Object { $_.Linearity -ne $null -and $_.Linearity -gt 0.1 }
    if ($nonlinear) {
        Write-Host ""
        Write-Host "  Not linear in caller tokens, so a per-token basis does not describe these:" -ForegroundColor Yellow
        foreach ($row in $nonlinear) { Write-Host ("    {0}: mid-point off by {1:P0}" -f $row.Model, $row.Linearity) -ForegroundColor Yellow }
    }
}

$failed = $results | Where-Object { $_.Status -notin @("ok", "partial") }
if ($failed) {
    Write-Host ""
    Write-Host ("  {0} model(s) not measured: {1}" -f $failed.Count, (($failed | ForEach-Object { "$($_.Model) ($($_.Status))" }) -join ", ")) -ForegroundColor Yellow
}

if ($JsonOut) {
    $payload = [pscustomobject]@{
        measuredAt            = (Get-Date).ToString("o")
        baseUrl               = $BaseUrl
        prefix                = $Prefix
        repeat                = $Repeat
        sizesInParagraphs     = $SizesInParagraphs
        referenceCallerTokens = $ReferenceCallerTokens
        basis                 = [pscustomobject]@{
            retailPerMillion = $RetailPerMillion; upstreamPerMillion = $UpstreamPerMillion
            retailFeeVnd = $RetailFeeVnd; upstreamFeeVnd = $UpstreamFeeVnd
        }
        results               = $results
    }
    $payload | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $JsonOut -Encoding UTF8
    Write-Host ""
    Write-Host "Wrote $JsonOut" -ForegroundColor Cyan
}
