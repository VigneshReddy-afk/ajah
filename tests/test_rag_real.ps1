# test_rag_real.ps1
# End-to-end RAG verification tests against a live Ajah gateway.
# Requires: docker-compose up -d, GROQ_API_KEY set.
#
# Usage:
#   $env:GROQ_API_KEY = "gsk_..."
#   powershell -ExecutionPolicy Bypass -File tests\test_rag_real.ps1
#
# Note: the gateway stores rag_verdict in ClickHouse but does not yet expose it
# in the /metrics/traces API. Tests infer the RAG outcome from:
#   grounding_score  -- higher is more grounded (>= 0.4 = likely "supported")
#   risk_level       -- "low" / "medium" / "high"
#   should_warn      -- true when hallucination risk or poor grounding detected
#   reasons[]        -- from GET /warnings/{id}, scorer explanation

param(
    [string]$GatewayURL = "http://localhost:8080",
    [int]   $WaitSec    = 8
)

$ErrorActionPreference = "Continue"

# ── Preflight ─────────────────────────────────────────────────────────────────

$GroqKey = $env:GROQ_API_KEY
if (-not $GroqKey) {
    Write-Host "ERROR: GROQ_API_KEY environment variable is not set." -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "Ajah RAG + Cross-Model End-to-End Tests" -ForegroundColor Cyan
Write-Host "Gateway : $GatewayURL"
Write-Host "Wait    : ${WaitSec}s per request"
Write-Host ""

# ── Helpers ───────────────────────────────────────────────────────────────────

function New-RequestID {
    [System.Guid]::NewGuid().ToString("N").Substring(0, 16)
}

function To-Base64 {
    param([string]$Text)
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($Text)
    [System.Convert]::ToBase64String($bytes)
}

function Send-Chat {
    param(
        [string]$Model,
        [string]$UserMessage,
        [string]$RequestID,
        [string]$FeatureName,
        [string]$SourceContextB64 = ""
    )

    $body = ConvertTo-Json -Depth 5 -InputObject @{
        model    = $Model
        messages = @( @{ role = "user"; content = $UserMessage } )
    }

    $headers = @{
        "Authorization"  = "Bearer $GroqKey"
        "Content-Type"   = "application/json"
        "X-Request-ID"   = $RequestID
        "X-Feature-Name" = $FeatureName
    }
    if ($SourceContextB64 -ne "") {
        $headers["X-Source-Context"] = $SourceContextB64
    }

    try {
        $resp = Invoke-RestMethod `
            -Uri     "$GatewayURL/v1/chat/completions" `
            -Method  Post `
            -Headers $headers `
            -Body    $body
        return $resp.choices[0].message.content
    } catch {
        Write-Host "  [ERROR] LLM call failed: $($_.Exception.Message)" -ForegroundColor Red
        return ""
    }
}

function Get-Trace {
    param([string]$RequestID)
    try {
        $traces = Invoke-RestMethod -Uri "$GatewayURL/metrics/traces" -Method Get
        foreach ($t in $traces) {
            if ($t.request_id -eq $RequestID) { return $t }
        }
        return $null
    } catch {
        return $null
    }
}

function Get-Flag {
    param([string]$RequestID)
    try {
        return Invoke-RestMethod -Uri "$GatewayURL/warnings/$RequestID" -Method Get
    } catch {
        return $null
    }
}

function Show-Trace {
    param($Trace, $Flag)
    if ($null -eq $Trace) {
        Write-Host "  trace        : not found in /metrics/traces" -ForegroundColor Yellow
        return
    }
    $gs  = "{0:F3}" -f $Trace.grounding_score
    $hr  = "{0:F3}" -f $Trace.hallucination_risk
    Write-Host "  risk_level   : $($Trace.risk_level)"
    Write-Host "  grounding    : $gs"
    Write-Host "  hallucination: $hr"
    Write-Host "  should_warn  : $($Trace.should_warn)"
    if ($null -ne $Flag -and $null -ne $Flag.reasons -and $Flag.reasons.Count -gt 0) {
        Write-Host "  reasons      : $($Flag.reasons -join ' | ')"
    }
}

function Write-Pass { param([string]$Label); Write-Host "  [PASS] $Label" -ForegroundColor Green }
function Write-Fail { param([string]$Label); Write-Host "  [FAIL] $Label" -ForegroundColor Red }

$Passed = 0
$Total  = 4
$Model  = "llama-3.3-70b-versatile"

# ─────────────────────────────────────────────────────────────────────────────
# TEST 1 -- Supported claim
# ─────────────────────────────────────────────────────────────────────────────

Write-Host "TEST 1 -- Supported claim (expect: risk_level=low, grounding >= 0.4)" -ForegroundColor White
Write-Host "-----------------------------------------------------------------------"

$Source1 = "Ajah is an open source LLM observability platform built in Go. " +
           "It acts as a gateway proxy that sits between your application and any LLM provider API. " +
           "Ajah supports exactly 9 providers: OpenAI, Anthropic, Groq, Google Gemini, xAI Grok, " +
           "Mistral, Together AI, NVIDIA, and Cohere. Provider detection is automatic based on API " +
           "key prefix. Ajah adds less than 2 milliseconds of latency overhead. It is deployed using " +
           "a single docker-compose up command. All services including the gateway, scorer, dashboard, " +
           "Redis, PostgreSQL, and ClickHouse start together. The dashboard runs on port 3000. The " +
           "gateway runs on port 8080. Ajah is completely free and open source under the MIT license. " +
           "No data leaves the user's server. Ajah was built to replace Helicone after it went into " +
           "maintenance mode."
$Enc1 = To-Base64 $Source1
$RID1 = New-RequestID
$Q1   = "What is Ajah, how many providers does it support, and how is it deployed?"

Write-Host "  Sending request (id=$RID1)..."
$Answer1 = Send-Chat -Model $Model -UserMessage $Q1 -RequestID $RID1 `
                     -FeatureName "rag-test-supported" -SourceContextB64 $Enc1

if ($Answer1 -eq "") {
    Write-Fail "LLM call failed -- skipping"
} else {
    $preview = if ($Answer1.Length -gt 120) { $Answer1.Substring(0, 120) + "..." } else { $Answer1 }
    Write-Host "  LLM answer   : $preview"
    Write-Host "  Waiting ${WaitSec}s for async scoring..."
    Start-Sleep -Seconds $WaitSec

    $T1 = Get-Trace $RID1
    $F1 = Get-Flag  $RID1
    Show-Trace $T1 $F1

    if ($null -ne $T1 -and $T1.risk_level -eq "low" -and $T1.grounding_score -ge 0.4) {
        $gs = "{0:F3}" -f $T1.grounding_score
        Write-Pass "risk_level=low, grounding=$gs (supported)"
        $Passed++
    } elseif ($null -ne $T1) {
        $gs = "{0:F3}" -f $T1.grounding_score
        Write-Fail "risk_level=$($T1.risk_level), grounding=$gs (expected low + grounding >= 0.4)"
    } else {
        Write-Fail "trace not found -- pipeline may still be processing"
    }
}
Write-Host ""

# ─────────────────────────────────────────────────────────────────────────────
# TEST 2 -- Unsupported claim
# ─────────────────────────────────────────────────────────────────────────────

Write-Host "TEST 2 -- Unsupported claim (expect: should_warn=true or grounding < 0.45)" -ForegroundColor White
Write-Host "----------------------------------------------------------------------------"

$Enc2 = $Enc1
$RID2 = New-RequestID
$Q2   = "What is Ajah's monthly pricing and where are their data centers located?"

Write-Host "  Sending request (id=$RID2)..."
$Answer2 = Send-Chat -Model $Model -UserMessage $Q2 -RequestID $RID2 `
                     -FeatureName "rag-test-unsupported" -SourceContextB64 $Enc2

if ($Answer2 -eq "") {
    Write-Fail "LLM call failed -- skipping"
} else {
    $preview = if ($Answer2.Length -gt 120) { $Answer2.Substring(0, 120) + "..." } else { $Answer2 }
    Write-Host "  LLM answer   : $preview"
    Write-Host "  Waiting ${WaitSec}s for async scoring..."
    Start-Sleep -Seconds $WaitSec

    $T2 = Get-Trace $RID2
    $F2 = Get-Flag  $RID2
    Show-Trace $T2 $F2

    if ($null -ne $T2 -and ($T2.should_warn -or $T2.risk_level -ne "low" -or $T2.grounding_score -lt 0.45)) {
        $gs = "{0:F3}" -f $T2.grounding_score
        Write-Pass "should_warn=$($T2.should_warn), risk=$($T2.risk_level), grounding=$gs (unsupported)"
        $Passed++
    } elseif ($null -ne $T2) {
        $gs = "{0:F3}" -f $T2.grounding_score
        Write-Fail "grounding=$gs, risk=$($T2.risk_level) -- scorer did not flag unsupported claims"
    } else {
        Write-Fail "trace not found"
    }
}
Write-Host ""

# ─────────────────────────────────────────────────────────────────────────────
# TEST 3 -- Contradicted claim
# ─────────────────────────────────────────────────────────────────────────────

Write-Host "TEST 3 -- Contradicted claim (expect: risk=high or medium, should_warn=true)" -ForegroundColor White
Write-Host "------------------------------------------------------------------------------"

$Source3 = "Ajah is completely free and open source. There is no paid plan."
$Enc3    = To-Base64 $Source3
$RID3    = New-RequestID
$Q3      = "Tell me about Ajah's enterprise paid plans and pricing tiers."

Write-Host "  Sending request (id=$RID3)..."
$Answer3 = Send-Chat -Model $Model -UserMessage $Q3 -RequestID $RID3 `
                     -FeatureName "rag-test-contradicted" -SourceContextB64 $Enc3

if ($Answer3 -eq "") {
    Write-Fail "LLM call failed -- skipping"
} else {
    $preview = if ($Answer3.Length -gt 120) { $Answer3.Substring(0, 120) + "..." } else { $Answer3 }
    Write-Host "  LLM answer   : $preview"
    Write-Host "  Waiting ${WaitSec}s for async scoring..."
    Start-Sleep -Seconds $WaitSec

    $T3 = Get-Trace $RID3
    $F3 = Get-Flag  $RID3
    Show-Trace $T3 $F3

    if ($null -ne $T3 -and $T3.should_warn -and ($T3.risk_level -eq "high" -or $T3.risk_level -eq "medium")) {
        Write-Pass "should_warn=true, risk=$($T3.risk_level) (contradicted)"
        $Passed++
    } elseif ($null -ne $T3) {
        Write-Fail "risk=$($T3.risk_level), should_warn=$($T3.should_warn) -- expected contradiction flag"
    } else {
        Write-Fail "trace not found"
    }
}
Write-Host ""

# ─────────────────────────────────────────────────────────────────────────────
# TEST 4 -- Cross-model verification
# ─────────────────────────────────────────────────────────────────────────────

Write-Host "TEST 4 -- Cross-model verification (expect: cross_model_verdict non-empty)" -ForegroundColor White
Write-Host "----------------------------------------------------------------------------"

Write-Host "  Configuring cross-model for feature 'cross-model-test'..."
$settingsBody = ConvertTo-Json -Depth 6 -InputObject @{
    feature_settings = @(
        @{
            feature_name             = "cross-model-test"
            cross_model_enabled      = $true
            cross_model_provider_url = "https://api.groq.com/openai/v1"
            cross_model_api_key      = $GroqKey
            cross_model_model        = "llama-3.1-8b-instant"
            cost_alert_threshold_usd = 1.0
            pii_masking_enabled      = $false
            webhook_url              = ""
        }
    )
    provider_keys = @()
}

try {
    $sr = Invoke-RestMethod `
        -Uri     "$GatewayURL/settings" `
        -Method  Post `
        -Headers @{ "Content-Type" = "application/json" } `
        -Body    $settingsBody
    Write-Host "  Settings saved: ok=$($sr.ok)"
} catch {
    Write-Host "  [WARN] POST /settings failed: $($_.Exception.Message)" -ForegroundColor Yellow
}

Start-Sleep -Seconds 2

$RID4 = New-RequestID
$Q4   = "What year was the Eiffel Tower built and how tall is it in meters?"

Write-Host "  Primary model  : $Model"
Write-Host "  Secondary model: llama-3.1-8b-instant"
Write-Host "  Sending request (id=$RID4)..."

$Answer4 = Send-Chat -Model $Model -UserMessage $Q4 -RequestID $RID4 -FeatureName "cross-model-test"

if ($Answer4 -eq "") {
    Write-Fail "LLM call failed -- skipping"
} else {
    $preview = if ($Answer4.Length -gt 120) { $Answer4.Substring(0, 120) + "..." } else { $Answer4 }
    Write-Host "  LLM answer   : $preview"
    Write-Host "  Waiting ${WaitSec}s for async scoring + cross-model call..."
    Start-Sleep -Seconds $WaitSec

    $T4 = Get-Trace $RID4
    if ($null -eq $T4) {
        Write-Fail "trace not found"
    } else {
        $verdict   = $T4.cross_model_verdict
        $agreement = "{0:F3}" -f $T4.cross_model_agreement
        $empty     = ($null -eq $verdict -or $verdict -eq "")

        Write-Host "  cross_model_verdict  : $(if ($empty) { '(empty)' } else { $verdict })"
        Write-Host "  cross_model_agreement: $agreement"
        Write-Host "  risk_level           : $($T4.risk_level)"

        if (-not $empty) {
            Write-Pass "cross_model_verdict=$verdict, agreement=$agreement"
            $Passed++
        } else {
            Write-Fail "cross_model_verdict is empty -- check settings were applied and cross-model is enabled"
        }
    }
}
Write-Host ""

# ─────────────────────────────────────────────────────────────────────────────
# Summary
# ─────────────────────────────────────────────────────────────────────────────

$color = if ($Passed -eq $Total) { "Green" } elseif ($Passed -ge 2) { "Yellow" } else { "Red" }
Write-Host "==================================" -ForegroundColor $color
Write-Host "  $Passed / $Total tests passed"   -ForegroundColor $color
Write-Host "==================================" -ForegroundColor $color
Write-Host ""

if ($Passed -lt $Total) {
    Write-Host "Troubleshooting tips:" -ForegroundColor Yellow
    Write-Host "  - Check gateway health : Invoke-RestMethod $GatewayURL/health"
    Write-Host "  - Check scorer health  : Invoke-RestMethod http://localhost:8001/health"
    Write-Host "  - Scorer logs          : docker-compose logs scorer"
    Write-Host "  - Increase wait time   : -WaitSec 15"
    Write-Host ""
}

if ($Passed -eq $Total) { exit 0 } else { exit 1 }
