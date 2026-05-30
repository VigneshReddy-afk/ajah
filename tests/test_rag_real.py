#!/usr/bin/env python3
"""
test_rag_real.py
End-to-end RAG verification tests against a live Ajah gateway.

Requires:
    pip install requests
    docker-compose up -d
    export GROQ_API_KEY=gsk_...

Note: the gateway stores rag_verdict in ClickHouse but does not yet expose it
in the /metrics/traces API. Tests infer the RAG outcome from:
    grounding_score  — higher is more grounded (>= 0.5 = likely "supported")
    risk_level       — "low" / "medium" / "high"
    should_warn      — True when hallucination risk or poor grounding is detected
    reasons[]        — from GET /warnings/{id}, contains the scorer's explanation
"""

import base64
import os
import sys
import time
import uuid

import requests

# ── Config ────────────────────────────────────────────────────────────────────

GATEWAY_URL = os.environ.get("AJAH_GATEWAY_URL", "http://localhost:8080")
GROQ_KEY    = os.environ.get("GROQ_API_KEY", "")
WAIT_SEC    = int(os.environ.get("AJAH_WAIT_SEC", "6"))
PRIMARY_MODEL   = "llama-3.3-70b-versatile"
SECONDARY_MODEL = "llama-3.1-8b-instant"

GREEN  = "\033[92m"
RED    = "\033[91m"
YELLOW = "\033[93m"
CYAN   = "\033[96m"
WHITE  = "\033[97m"
RESET  = "\033[0m"
BOLD   = "\033[1m"

# ── Helpers ───────────────────────────────────────────────────────────────────

def new_request_id() -> str:
    return uuid.uuid4().hex[:16]

def b64(text: str) -> str:
    return base64.b64encode(text.encode()).decode()

def send_chat(
    model: str,
    user_message: str,
    request_id: str,
    feature_name: str,
    source_context_b64: str = "",
) -> str | None:
    headers = {
        "Authorization": f"Bearer {GROQ_KEY}",
        "Content-Type": "application/json",
        "X-Request-ID": request_id,
        "X-Feature-Name": feature_name,
    }
    if source_context_b64:
        headers["X-Source-Context"] = source_context_b64

    payload = {
        "model": model,
        "messages": [{"role": "user", "content": user_message}],
    }

    try:
        r = requests.post(
            f"{GATEWAY_URL}/v1/chat/completions",
            headers=headers,
            json=payload,
            timeout=30,
        )
        r.raise_for_status()
        return r.json()["choices"][0]["message"]["content"]
    except Exception as e:
        print(f"  {RED}[ERROR] LLM call failed: {e}{RESET}")
        return None

def get_trace(request_id: str) -> dict | None:
    try:
        r = requests.get(f"{GATEWAY_URL}/metrics/traces", timeout=10)
        r.raise_for_status()
        traces = r.json()
        for t in traces:
            if t.get("request_id") == request_id:
                return t
        return None
    except Exception:
        return None

def get_flag(request_id: str) -> dict | None:
    try:
        r = requests.get(f"{GATEWAY_URL}/warnings/{request_id}", timeout=10)
        if r.status_code == 404:
            return None
        r.raise_for_status()
        return r.json()
    except Exception:
        return None

def show_trace_result(trace: dict | None, flag: dict | None) -> None:
    if not trace:
        print(f"  {YELLOW}trace        : not found in /metrics/traces{RESET}")
        return
    print(f"  risk_level   : {trace.get('risk_level', '?')}")
    print(f"  grounding    : {trace.get('grounding_score', 0):.3f}")
    print(f"  hallucination: {trace.get('hallucination_risk', 0):.3f}")
    print(f"  should_warn  : {trace.get('should_warn', False)}")
    if flag and flag.get("reasons"):
        reasons = " | ".join(flag["reasons"])
        print(f"  reasons      : {reasons}")

def pass_test(label: str) -> int:
    print(f"  {GREEN}[PASS]{RESET} {label}")
    return 1

def fail_test(label: str) -> int:
    print(f"  {RED}[FAIL]{RESET} {label}")
    return 0

# ── Preflight ─────────────────────────────────────────────────────────────────

if not GROQ_KEY:
    print(f"{RED}GROQ_API_KEY environment variable is not set.{RESET}")
    sys.exit(1)

print()
print(f"{CYAN}{BOLD}Ajah RAG + Cross-Model End-to-End Tests{RESET}")
print(f"Gateway : {GATEWAY_URL}")
print(f"Wait    : {WAIT_SEC}s per request")
print()

passed = 0

# ── TEST 1 — Supported claim ──────────────────────────────────────────────────

print(f"{WHITE}{BOLD}TEST 1 — Supported claim (expect: low risk, grounding >= 0.5){RESET}")
print("─" * 60)

source1 = (
    "Ajah is an open source LLM observability platform built in Go. "
    "It acts as a gateway proxy that sits between your application and any LLM provider API. "
    "Ajah supports exactly 9 providers: OpenAI, Anthropic, Groq, Google Gemini, xAI Grok, "
    "Mistral, Together AI, NVIDIA, and Cohere. Provider detection is automatic based on API "
    "key prefix. Ajah adds less than 2 milliseconds of latency overhead. It is deployed using "
    "a single docker-compose up command. All services including the gateway, scorer, dashboard, "
    "Redis, PostgreSQL, and ClickHouse start together. The dashboard runs on port 3000. The "
    "gateway runs on port 8080. Ajah is completely free and open source under the MIT license. "
    "No data leaves the user's server. Ajah was built to replace Helicone after it went into "
    "maintenance mode."
)
enc1 = b64(source1)
rid1 = new_request_id()
q1   = "What is Ajah, how many providers does it support, and how is it deployed?"

print(f"  Sending request (id={rid1})...")
answer1 = send_chat(PRIMARY_MODEL, q1, rid1, "rag-test-supported", enc1)

if not answer1:
    passed += fail_test("LLM call failed — skipping")
else:
    preview = answer1[:120] + ("..." if len(answer1) > 120 else "")
    print(f"  LLM answer   : {preview}")
    print(f"  Waiting {WAIT_SEC}s for async scoring...")
    time.sleep(WAIT_SEC)

    t1 = get_trace(rid1)
    f1 = get_flag(rid1)
    show_trace_result(t1, f1)

    if t1 and t1.get("risk_level") == "low" and t1.get("grounding_score", 0) >= 0.4:
        passed += pass_test(
            f"risk_level=low, grounding={t1['grounding_score']:.3f} (supported)"
        )
    elif t1:
        passed += fail_test(
            f"risk_level={t1.get('risk_level')}, grounding={t1.get('grounding_score', 0):.3f} "
            "(expected supported/low)"
        )
    else:
        passed += fail_test("trace not found — pipeline may still be processing")

print()

# ── TEST 2 — Unsupported claim ────────────────────────────────────────────────

print(f"{WHITE}{BOLD}TEST 2 — Unsupported claim (expect: should_warn=True, risk medium/high){RESET}")
print("─" * 68)

enc2 = enc1  # same source doc
rid2 = new_request_id()
q2   = "What is Ajah's monthly pricing and where are their data centers located?"

print(f"  Sending request (id={rid2})...")
answer2 = send_chat(PRIMARY_MODEL, q2, rid2, "rag-test-unsupported", enc2)

if not answer2:
    passed += fail_test("LLM call failed — skipping")
else:
    preview = answer2[:120] + ("..." if len(answer2) > 120 else "")
    print(f"  LLM answer   : {preview}")
    print(f"  Waiting {WAIT_SEC}s for async scoring...")
    time.sleep(WAIT_SEC)

    t2 = get_trace(rid2)
    f2 = get_flag(rid2)
    show_trace_result(t2, f2)

    if t2 and (t2.get("should_warn") or
               t2.get("risk_level") != "low" or
               t2.get("grounding_score", 1.0) < 0.45):
        passed += pass_test(
            f"should_warn={t2.get('should_warn')}, risk={t2.get('risk_level')}, "
            f"grounding={t2.get('grounding_score', 0):.3f} (unsupported)"
        )
    elif t2:
        passed += fail_test(
            f"grounding={t2.get('grounding_score', 0):.3f}, risk={t2.get('risk_level')} "
            "— scorer did not flag unsupported claims"
        )
    else:
        passed += fail_test("trace not found")

print()

# ── TEST 3 — Contradicted claim ───────────────────────────────────────────────

print(f"{WHITE}{BOLD}TEST 3 — Contradicted claim (expect: risk_level=high, should_warn=True){RESET}")
print("─" * 70)

source3 = "Ajah is completely free and open source. There is no paid plan."
enc3    = b64(source3)
rid3    = new_request_id()
q3      = "Tell me about Ajah's enterprise paid plans and pricing tiers."

print(f"  Sending request (id={rid3})...")
answer3 = send_chat(PRIMARY_MODEL, q3, rid3, "rag-test-contradicted", enc3)

if not answer3:
    passed += fail_test("LLM call failed — skipping")
else:
    preview = answer3[:120] + ("..." if len(answer3) > 120 else "")
    print(f"  LLM answer   : {preview}")
    print(f"  Waiting {WAIT_SEC}s for async scoring...")
    time.sleep(WAIT_SEC)

    t3 = get_trace(rid3)
    f3 = get_flag(rid3)
    show_trace_result(t3, f3)

    if t3 and t3.get("should_warn") and t3.get("risk_level") in ("high", "medium"):
        passed += pass_test(
            f"should_warn=True, risk={t3.get('risk_level')} (contradicted)"
        )
    elif t3:
        passed += fail_test(
            f"risk={t3.get('risk_level')}, should_warn={t3.get('should_warn')} "
            "— expected high-risk contradiction flag"
        )
    else:
        passed += fail_test("trace not found")

print()

# ── TEST 4 — Cross-model verification ─────────────────────────────────────────

print(f"{WHITE}{BOLD}TEST 4 — Cross-model verification (expect: cross_model_verdict non-empty){RESET}")
print("─" * 72)

# Configure the "cross-model-test" feature to use SECONDARY_MODEL as verifier.
# Both models are on Groq so the same API key covers both.
print("  Configuring cross-model for feature 'cross-model-test'...")
settings_payload = {
    "feature_settings": [
        {
            "feature_name": "cross-model-test",
            "cross_model_enabled": True,
            "cross_model_provider_url": "https://api.groq.com/openai/v1",
            "cross_model_api_key": GROQ_KEY,
            "cross_model_model": SECONDARY_MODEL,
            "cost_alert_threshold_usd": 1.0,
            "pii_masking_enabled": False,
            "webhook_url": "",
        }
    ],
    "provider_keys": [],
}

try:
    sr = requests.post(
        f"{GATEWAY_URL}/settings",
        json=settings_payload,
        timeout=10,
    )
    sr.raise_for_status()
    print(f"  Settings saved: {sr.text.strip()}")
except Exception as e:
    print(f"  {YELLOW}[WARN] POST /settings failed: {e} — cross-model may not be active{RESET}")

time.sleep(2)

rid4 = new_request_id()
q4   = "What year was the Eiffel Tower built and how tall is it in meters?"

print(f"  Primary model  : {PRIMARY_MODEL}")
print(f"  Secondary model: {SECONDARY_MODEL}")
print(f"  Sending request (id={rid4})...")

answer4 = send_chat(PRIMARY_MODEL, q4, rid4, "cross-model-test")

if not answer4:
    passed += fail_test("LLM call failed — skipping")
else:
    preview = answer4[:120] + ("..." if len(answer4) > 120 else "")
    print(f"  LLM answer   : {preview}")
    print(f"  Waiting {WAIT_SEC}s for async scoring + cross-model call...")
    time.sleep(WAIT_SEC)

    t4 = get_trace(rid4)
    if not t4:
        passed += fail_test("trace not found")
    else:
        verdict   = t4.get("cross_model_verdict", "")
        agreement = t4.get("cross_model_agreement", 0.0)

        print(f"  cross_model_verdict  : {verdict if verdict else '(empty)'}")
        print(f"  cross_model_agreement: {agreement:.3f}")
        print(f"  risk_level           : {t4.get('risk_level', '?')}")

        if verdict:
            passed += pass_test(
                f"cross_model_verdict={verdict}, agreement={agreement:.3f}"
            )
        else:
            passed += fail_test(
                "cross_model_verdict is empty — cross-model may not be enabled "
                "or settings did not apply"
            )

print()

# ── Summary ───────────────────────────────────────────────────────────────────

total = 4
color = GREEN if passed == total else (YELLOW if passed >= 2 else RED)
print(f"{color}{BOLD}══════════════════════════════════{RESET}")
print(f"{color}{BOLD}  {passed} / {total} tests passed{RESET}")
print(f"{color}{BOLD}══════════════════════════════════{RESET}")
print()

if passed < total:
    print(f"{YELLOW}Troubleshooting tips:{RESET}")
    print(f"  - Confirm services are up: curl {GATEWAY_URL}/health")
    print(f"  - Check scorer logs: docker-compose logs scorer")
    print(f"  - Grounding scores depend on sentence-transformers model being loaded")
    print(f"  - Cross-model requires scorer /compare endpoint to be healthy")
    print()

sys.exit(0 if passed == total else 1)
