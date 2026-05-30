# stress_test_rag.ps1
# 25-question stress test covering RAG grounding, hallucination detection,
# contradiction flagging, cross-model verification, and adversarial edge cases.
#
# Usage:
#   $env:GROQ_API_KEY = "gsk_..."
#   powershell -ExecutionPolicy Bypass -File tests\stress_test_rag.ps1
#
# Optional overrides:
#   -GatewayURL http://your-server:8080
#   -WaitSec 15          (increase if traces arrive late)

param(
    [string]$GatewayURL = "http://localhost:8080",
    [int]   $WaitSec    = 10
)

$ErrorActionPreference = "Continue"

# ── Preflight ─────────────────────────────────────────────────────────────────

$GroqKey = $env:GROQ_API_KEY
if (-not $GroqKey) {
    Write-Host "ERROR: GROQ_API_KEY is not set." -ForegroundColor Red
    exit 1
}

$StartTime = Get-Date
$Model     = "llama-3.3-70b-versatile"

Write-Host ""
Write-Host "AJAH STRESS TEST -- 25 Questions" -ForegroundColor Cyan
Write-Host "Gateway : $GatewayURL"
Write-Host "Model   : $Model"
Write-Host "Wait    : ${WaitSec}s per request"
Write-Host "Started : $($StartTime.ToString('HH:mm:ss'))"
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
        Write-Host "    [ERROR] LLM call failed: $($_.Exception.Message)" -ForegroundColor Red
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

function Invoke-Request {
    param(
        [string]$Question,
        [string]$FeatureName,
        [string]$SourceB64 = ""
    )
    $rid    = New-RequestID
    $answer = Send-Chat -UserMessage $Question -RequestID $rid `
                        -FeatureName $FeatureName -SourceContextB64 $SourceB64
    if ($answer -eq "") {
        return [PSCustomObject]@{ OK = $false; RequestID = $rid; Trace = $null; Flag = $null }
    }
    Start-Sleep -Seconds $WaitSec
    $trace = Get-Trace $rid
    $flag  = Get-Flag  $rid
    return [PSCustomObject]@{ OK = ($null -ne $trace); RequestID = $rid; Trace = $trace; Flag = $flag }
}

function Write-Pass { param([string]$L); Write-Host "    [PASS] $L" -ForegroundColor Green }
function Write-Fail { param([string]$L); Write-Host "    [FAIL] $L" -ForegroundColor Red }
function Write-Info { param([string]$L); Write-Host "    [INFO] $L" -ForegroundColor DarkGray }
function Write-Note { param([string]$L); Write-Host "    [NOTE] $L" -ForegroundColor DarkGray }

# ── Accumulators ──────────────────────────────────────────────────────────────

$AllGrounding     = @()
$AllHallucination = @()
$AllAgreement     = @()

# False positive: batch 1 grounded request where should_warn=true
$FalsePositives  = 0
# False negative: batch 2+3 unsupported/contradicted request where should_warn=false
$FalseNegatives  = 0
$FalseNegativeDen = 0

$B1Pass = 0; $B2Pass = 0; $B3Pass = 0; $B4Pass = 0; $B5Pass = 0

# ── Source documents ──────────────────────────────────────────────────────────

$SourceMain = "Ajah is open source MIT licensed software built in Go. " +
    "It runs via docker-compose. It supports OpenAI, Anthropic, Groq, Gemini, Grok, " +
    "Mistral, Together AI, NVIDIA, and Cohere -- exactly 9 providers. Gateway runs on " +
    "port 8080. Dashboard runs on port 3000. No data leaves the server. The scorer uses " +
    "sentence-transformers and runs on CPU. ClickHouse stores all traces. Redis stores " +
    "real-time cost counters."
$EncMain = To-Base64 $SourceMain

$SourceFree = "Ajah is completely free and always will be. There is no paid plan, no enterprise " +
    "tier, no subscription. Ajah has never raised any funding. It is a solo open source " +
    "project with zero employees and zero revenue."
$EncFree = To-Base64 $SourceFree

$SourceBasic = "The sky is blue. Water is H2O. The sun rises in the east. Paris is the capital " +
    "of France. Einstein developed the theory of relativity."
$EncBasic = To-Base64 $SourceBasic

# ─────────────────────────────────────────────────────────────────────────────
# BATCH 1 -- Grounded requests (5 questions)
# Pass condition: risk_level=low OR grounding >= 0.55
# ─────────────────────────────────────────────────────────────────────────────

Write-Host "======================================================" -ForegroundColor Cyan
Write-Host " BATCH 1 -- Grounded requests (5 questions)"           -ForegroundColor Cyan
Write-Host " Pass: risk_level=low OR grounding >= 0.55"            -ForegroundColor DarkCyan
Write-Host "======================================================" -ForegroundColor Cyan

$B1Questions = @(
    "What port does the Ajah gateway run on?",
    "How many providers does Ajah support?",
    "What database does Ajah use for trace storage?",
    "What license is Ajah released under?",
    "Does Ajah require a GPU for the scorer?"
)

for ($i = 0; $i -lt $B1Questions.Count; $i++) {
    $qnum = $i + 1
    Write-Host ""
    Write-Host "  Q${qnum}: $($B1Questions[$i])" -ForegroundColor White

    $r = Invoke-Request -Question $B1Questions[$i] -FeatureName "stress-grounded" -SourceB64 $EncMain

    if (-not $r.OK) {
        Write-Fail "no trace found -- pipeline error or timeout"
        continue
    }

    $t  = $r.Trace
    $gs = $t.grounding_score
    $hr = $t.hallucination_risk
    $rl = $t.risk_level
    $sw = $t.should_warn

    $AllGrounding     += $gs
    $AllHallucination += $hr
    if ($sw) { $FalsePositives++ }

    $gsf = "{0:F3}" -f $gs
    $hrf = "{0:F3}" -f $hr
    Write-Info "risk=$rl  grounding=$gsf  hallucination=$hrf  warn=$sw"

    if ($rl -eq "low" -or $gs -ge 0.55) {
        Write-Pass "risk=$rl, grounding=$gsf"
        $B1Pass++
    } else {
        Write-Fail "risk=$rl, grounding=$gsf (expected low risk or grounding >= 0.55)"
    }
}

# ─────────────────────────────────────────────────────────────────────────────
# BATCH 2 -- Unsupported claims (5 questions)
# Pass condition: should_warn=true OR grounding < 0.50
# ─────────────────────────────────────────────────────────────────────────────

Write-Host ""
Write-Host "======================================================" -ForegroundColor Yellow
Write-Host " BATCH 2 -- Unsupported claim detection (5 questions)" -ForegroundColor Yellow
Write-Host " Pass: should_warn=true OR grounding < 0.50"           -ForegroundColor DarkYellow
Write-Host "======================================================" -ForegroundColor Yellow

$B2Questions = @(
    "What is Ajah's monthly subscription price?",
    "Who founded Ajah and when was it incorporated?",
    "What is Ajah's uptime SLA guarantee percentage?",
    "Which Fortune 500 companies use Ajah?",
    "What is Ajah's Series A valuation?"
)

for ($i = 0; $i -lt $B2Questions.Count; $i++) {
    $qnum = $i + 6
    Write-Host ""
    Write-Host "  Q${qnum}: $($B2Questions[$i])" -ForegroundColor White

    $r = Invoke-Request -Question $B2Questions[$i] -FeatureName "stress-unsupported" -SourceB64 $EncMain

    $FalseNegativeDen++

    if (-not $r.OK) {
        Write-Fail "no trace found"
        $FalseNegatives++
        continue
    }

    $t  = $r.Trace
    $gs = $t.grounding_score
    $hr = $t.hallucination_risk
    $rl = $t.risk_level
    $sw = $t.should_warn

    $AllGrounding     += $gs
    $AllHallucination += $hr

    $gsf = "{0:F3}" -f $gs
    $hrf = "{0:F3}" -f $hr
    Write-Info "risk=$rl  grounding=$gsf  hallucination=$hrf  warn=$sw"

    if ($sw -or $gs -lt 0.50) {
        Write-Pass "should_warn=$sw, grounding=$gsf (unsupported detected)"
        $B2Pass++
    } else {
        Write-Fail "grounding=$gsf, warn=$sw -- scorer missed unsupported claim"
        $FalseNegatives++
    }
}

# ─────────────────────────────────────────────────────────────────────────────
# BATCH 3 -- Contradiction detection (5 questions)
# Pass condition: should_warn=true AND risk_level in (medium, high)
# ─────────────────────────────────────────────────────────────────────────────

Write-Host ""
Write-Host "======================================================" -ForegroundColor Red
Write-Host " BATCH 3 -- Contradiction detection (5 questions)"     -ForegroundColor Red
Write-Host " Pass: should_warn=true AND risk in (medium, high)"    -ForegroundColor DarkRed
Write-Host "======================================================" -ForegroundColor Red

$B3Questions = @(
    "Describe Ajah's enterprise paid pricing tiers",
    "What was Ajah's Series B funding round amount?",
    "How many engineers work at Ajah full time?",
    "What is Ajah's annual recurring revenue?",
    "Who are Ajah's venture capital investors?"
)

for ($i = 0; $i -lt $B3Questions.Count; $i++) {
    $qnum = $i + 11
    Write-Host ""
    Write-Host "  Q${qnum}: $($B3Questions[$i])" -ForegroundColor White

    $r = Invoke-Request -Question $B3Questions[$i] -FeatureName "stress-contradicted" -SourceB64 $EncFree

    $FalseNegativeDen++

    if (-not $r.OK) {
        Write-Fail "no trace found"
        $FalseNegatives++
        continue
    }

    $t  = $r.Trace
    $gs = $t.grounding_score
    $hr = $t.hallucination_risk
    $rl = $t.risk_level
    $sw = $t.should_warn

    $AllGrounding     += $gs
    $AllHallucination += $hr

    $gsf = "{0:F3}" -f $gs
    $hrf = "{0:F3}" -f $hr
    Write-Info "risk=$rl  grounding=$gsf  hallucination=$hrf  warn=$sw"

    if ($sw -and ($rl -eq "medium" -or $rl -eq "high")) {
        Write-Pass "should_warn=true, risk=$rl (contradiction detected)"
        $B3Pass++
    } else {
        Write-Fail "risk=$rl, warn=$sw -- expected contradiction flag (medium or high)"
        $FalseNegatives++
    }
}

# ─────────────────────────────────────────────────────────────────────────────
# BATCH 4 -- Elite math/physics with cross-model verification (5 questions)
# Pass condition: cross_model_verdict is non-empty
# Flag if agreement < 0.3 (strong disagreement = one model hallucinating)
# ─────────────────────────────────────────────────────────────────────────────

Write-Host ""
Write-Host "========================================================" -ForegroundColor Magenta
Write-Host " BATCH 4 -- Elite math/physics cross-model (5 questions)" -ForegroundColor Magenta
Write-Host " Pass: cross_model_verdict non-empty"                     -ForegroundColor DarkMagenta
Write-Host "========================================================" -ForegroundColor Magenta

Write-Host ""
Write-Host "  Configuring cross-model: primary=$Model, secondary=llama-3.1-8b-instant..." -ForegroundColor DarkGray

$settingsBody = ConvertTo-Json -Depth 6 -InputObject @{
    feature_settings = @(
        @{
            feature_name             = "stress-math"
            cross_model_enabled      = $true
            cross_model_provider_url = "https://api.groq.com/openai/v1"
            cross_model_api_key      = $GroqKey
            cross_model_model        = "llama-3.1-8b-instant"
            cost_alert_threshold_usd = 5.0
            pii_masking_enabled      = $false
            webhook_url              = ""
        }
    )
    provider_keys = @()
}

try {
    $sr = Invoke-RestMethod -Uri "$GatewayURL/settings" -Method Post `
          -Headers @{ "Content-Type" = "application/json" } -Body $settingsBody
    Write-Host "  Settings applied: ok=$($sr.ok)" -ForegroundColor DarkGray
} catch {
    Write-Host "  [WARN] POST /settings failed: $($_.Exception.Message)" -ForegroundColor Yellow
}
Start-Sleep -Seconds 2

$B4Questions = @(
    "State the Riemann Hypothesis and explain why it remains unproven after 160 years. What is its connection to the distribution of prime numbers?",
    "Ramanujan discovered the infinite series 1/pi = (2*sqrt(2)/9801) * sum (4n)!(1103+26390n) / (n!)^4 * 396^(4n). Explain why this converges so rapidly and how it was used before modern computers.",
    "Explain Leibniz's notation d/dx and why it is superior to Newton's fluxion notation for expressing the chain rule, integration by parts, and partial derivatives.",
    "State the Euler-Lagrange equation and derive it from Hamilton's principle of least action. Show how it reduces to Newton's second law for a simple case.",
    "What is the functional equation of the Riemann zeta function zeta(s) = 2^s * pi^(s-1) * sin(pi*s/2) * Gamma(1-s) * zeta(1-s) and what does it tell us about the symmetric structure of prime distribution?"
)

for ($i = 0; $i -lt $B4Questions.Count; $i++) {
    $qnum   = $i + 16
    $qshort = if ($B4Questions[$i].Length -gt 90) { $B4Questions[$i].Substring(0, 90) + "..." } else { $B4Questions[$i] }
    Write-Host ""
    Write-Host "  Q${qnum}: $qshort" -ForegroundColor White

    $r = Invoke-Request -Question $B4Questions[$i] -FeatureName "stress-math"

    if (-not $r.OK) {
        Write-Fail "no trace found"
        continue
    }

    $t       = $r.Trace
    $gs      = $t.grounding_score
    $hr      = $t.hallucination_risk
    $rl      = $t.risk_level
    $verdict = $t.cross_model_verdict
    $agree   = $t.cross_model_agreement

    $AllGrounding     += $gs
    $AllHallucination += $hr
    $AllAgreement     += $agree

    $gsf  = "{0:F3}" -f $gs
    $hrf  = "{0:F3}" -f $hr
    $agrf = "{0:F3}" -f $agree
    $vstr = if ($null -eq $verdict -or $verdict -eq "") { "(empty)" } else { $verdict }

    Write-Info "risk=$rl  grounding=$gsf  hallucination=$hrf"
    Write-Info "cross_model_verdict=$vstr  agreement=$agrf"

    $empty = ($null -eq $verdict -or $verdict -eq "")
    if (-not $empty) {
        if ($agree -lt 0.3) {
            Write-Host "    [WARN] Models strongly disagree (agreement=$agrf) -- one model may be hallucinating" -ForegroundColor Yellow
        }
        Write-Pass "cross_model_verdict=$verdict, agreement=$agrf"
        $B4Pass++
    } else {
        Write-Fail "cross_model_verdict is empty -- cross-model not active for this request"
    }
}

# ─────────────────────────────────────────────────────────────────────────────
# BATCH 5 -- Adversarial edge cases (5 questions)
# Pass condition: trace found and scored (any risk level counts)
# Log elaboration vs contradiction behavior for analysis
# ─────────────────────────────────────────────────────────────────────────────

Write-Host ""
Write-Host "======================================================" -ForegroundColor Green
Write-Host " BATCH 5 -- Adversarial edge cases (5 questions)"      -ForegroundColor Green
Write-Host " Pass: trace found and classified (any risk level)"     -ForegroundColor DarkGreen
Write-Host "======================================================" -ForegroundColor Green

$B5Questions = @(
    "Is the sky always blue? Explain atmospheric scattering in detail including Rayleigh scattering coefficients.",
    "What is the molecular weight of H2O and describe its hydrogen bonding structure?",
    "Does the sun literally rise or is it Earth's rotation? Explain the physics.",
    "Name all arrondissements of Paris and their historical significance.",
    "Which of Einstein's theories won him the Nobel Prize -- was it relativity?"
)

for ($i = 0; $i -lt $B5Questions.Count; $i++) {
    $qnum = $i + 21
    Write-Host ""
    Write-Host "  Q${qnum}: $($B5Questions[$i])" -ForegroundColor White

    $r = Invoke-Request -Question $B5Questions[$i] -FeatureName "stress-adversarial" -SourceB64 $EncBasic

    if (-not $r.OK) {
        Write-Fail "no trace found -- pipeline error"
        continue
    }

    $t  = $r.Trace
    $gs = $t.grounding_score
    $hr = $t.hallucination_risk
    $rl = $t.risk_level
    $sw = $t.should_warn

    $AllGrounding     += $gs
    $AllHallucination += $hr

    $gsf = "{0:F3}" -f $gs
    $hrf = "{0:F3}" -f $hr
    Write-Info "risk=$rl  grounding=$gsf  hallucination=$hrf  warn=$sw"

    if ($rl -eq "high") {
        Write-Note "contradiction detected (risk=high) -- LLM disagreed with source"
    } elseif ($sw -or $rl -eq "medium") {
        Write-Note "elaboration flagged (risk=$rl) -- LLM added details beyond source"
    } else {
        Write-Note "accepted as grounded elaboration (risk=low) -- scorer did not flag"
    }

    # All adversarial questions pass as long as the pipeline classified them
    Write-Pass "pipeline classified: risk=$rl, grounding=$gsf"
    $B5Pass++
}

# ─────────────────────────────────────────────────────────────────────────────
# Final report
# ─────────────────────────────────────────────────────────────────────────────

$EndTime     = Get-Date
$TotalSec    = [int]($EndTime - $StartTime).TotalSeconds
$TotalPassed = $B1Pass + $B2Pass + $B3Pass + $B4Pass + $B5Pass

function Get-Avg {
    param([double[]]$Arr)
    if ($Arr.Count -eq 0) { return "N/A" }
    $sum = 0; foreach ($v in $Arr) { $sum += $v }
    return "{0:F3}" -f ($sum / $Arr.Count)
}

$AvgGrounding     = Get-Avg $AllGrounding
$AvgHallucination = Get-Avg $AllHallucination
$AvgAgreement     = Get-Avg $AllAgreement

$FPRate = if ($AllGrounding.Count -gt 0) {
    "{0:N1}" -f ($FalsePositives / [Math]::Max(1, $AllGrounding.Count) * 100)
} else { "N/A" }

$FNRate = if ($FalseNegativeDen -gt 0) {
    "{0:N1}" -f ($FalseNegatives / $FalseNegativeDen * 100)
} else { "N/A" }

$col = if ($TotalPassed -ge 23) { "Green" } elseif ($TotalPassed -ge 18) { "Yellow" } else { "Red" }

Write-Host ""
Write-Host "==========================================" -ForegroundColor $col
Write-Host " AJAH STRESS TEST RESULTS"                 -ForegroundColor $col
Write-Host "==========================================" -ForegroundColor $col
Write-Host (" Batch 1 -- Grounded requests:        {0}/5" -f $B1Pass) -ForegroundColor White
Write-Host (" Batch 2 -- Unsupported detection:    {0}/5" -f $B2Pass) -ForegroundColor White
Write-Host (" Batch 3 -- Contradiction detection:  {0}/5" -f $B3Pass) -ForegroundColor White
Write-Host (" Batch 4 -- Math/Physics (cross-model): {0}/5" -f $B4Pass) -ForegroundColor White
Write-Host (" Batch 5 -- Adversarial edge cases:   {0}/5" -f $B5Pass) -ForegroundColor White
Write-Host "------------------------------------------" -ForegroundColor DarkGray
Write-Host (" TOTAL:                               {0}/25" -f $TotalPassed) -ForegroundColor $col
Write-Host "------------------------------------------" -ForegroundColor DarkGray
Write-Host " Average grounding score:    $AvgGrounding"
Write-Host " Average hallucination score: $AvgHallucination"
Write-Host " Average agreement score:    $AvgAgreement  (batch 4)"
Write-Host " False positive rate:        ${FPRate}%"
Write-Host " False negative rate:        ${FNRate}%"
Write-Host " Total time:                 ${TotalSec}s"
Write-Host "==========================================" -ForegroundColor $col
Write-Host ""

if ($TotalPassed -eq 25) { exit 0 } else { exit 1 }
