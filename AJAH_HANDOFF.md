# Ajah — Complete Project Handoff
**Date:** June 2, 2026  
**Founder:** Vignesh Reddy (VigneshReddy-afk)  
**Location:** Hyderabad, India

---

## What Ajah Is

An open-source, self-hostable LLM safety and observability platform.

**Tagline:** "The observatory at the edge of your AI universe — nothing escapes undetected."

**Core value proposition:** Every other LLM observability tool is cloud-locked or gets acquired. Ajah runs entirely on the customer's own server. No data leaves. No vendor dependency. No acquisition risk.

---

## Links

- **GitHub:** https://github.com/VigneshReddy-afk/ajah
- **Website:** https://useajah.com
- **Domain registrar:** Hostinger (useajah.com)
- **Hosting:** Cloudflare Pages (landing page only)
- **DNS:** Cloudflare nameservers (indie.ns.cloudflare.com, jose.ns.cloudflare.com)

---

## Tech Stack

| Component | Technology |
|---|---|
| Gateway proxy | Go 1.22 |
| Async pipeline | Go workers |
| Quality scorer | Python 3.11 + FastAPI |
| ML models | sentence-transformers/all-MiniLM-L6-v2, unitary/toxic-bert |
| Trace storage | ClickHouse |
| Real-time metrics | Redis |
| Config/settings | PostgreSQL |
| Dashboard | React 19 + TypeScript + Vite + Recharts |
| Deployment | Docker Compose |

---

## What's Built and Working

### Backend (Go)
- Gateway proxy — intercepts all LLM traffic, <2ms overhead
- 9 providers auto-detected from key prefix: OpenAI (sk-), Anthropic (sk-ant-), Groq (gsk_), Gemini (AIza), Grok (xai-), Mistral (mistral-), Together (together-), NVIDIA (nvapi-), Cohere (cohere-)
- Cost attribution engine — writes to Redis by user/feature/model
- PII masking — 6 types: EMAIL, PHONE, SSN, CREDIT_CARD, IP_ADDRESS, API_KEY
- Hallucination flagging — parallel scoring, zero latency added
- RAG verification — verifies responses against source documents via X-Source-Context header
- Cross-model verification — sends same prompt to secondary model, flags disagreements
- Claim density scoring — flags high-claim responses on low-context prompts, claim_density_risk float in ScoreResult
- Single scorer call refactor — duplicate HTTP call eliminated, full scorer result threaded through to Evaluate() and buildReasons()
- Specific warning reasons — "High claim density detected", "Response contradicts source document", "Cross-model disagreement detected" — not generic scores
- Multi-agent session tracing — groups calls by X-Session-ID, visual step tree
- Session reaper — closes idle sessions after 5 minutes, writes to ClickHouse
- Prometheus /metrics endpoint — 8 metrics live: ajah_requests_total, ajah_cost_usd_total, ajah_latency_ms, ajah_hallucination_risk, ajah_pii_detections_total, ajah_warnings_total, ajah_scorer_latency_ms, ajah_claim_density_risk

### Python Scorer (FastAPI on port 8001)
- Quality scoring: hallucination_score, factual_consistency_score, toxicity_score, overall_quality_score
- RAG verification: supported/partially_supported/unsupported/contradicted verdicts
- Claim density scoring: detects high-risk responses with many specific claims on low-context prompts
- claim_density_risk: float field in ScoreResult, threshold 0.6 triggers high_claim_density flag
- Claim definition: year/date assertions, statistics/percentages, proper nouns mid-sentence (4+ chars), absolute statements (always, never, definitely, certainly, guaranteed, proven, definitively, undoubtedly)
- Context multiplier: no source + <50 words = 1.0, no source + 50-200 words = 0.6, source present or >200 words = 0.2
- Models pre-baked into Docker image (no download on startup, ~15s start time)
- Health endpoint: GET /health returns models_loaded: true

### Dashboard (React, port 3000)
7 pages:
1. **Overview** — cost today, traces, PII detections, cost by feature/model charts, quality trend
2. **Traces** — live feed table, expandable rows showing masked prompt, RAG verdict, cross-model verdict
3. **Sessions** — multi-agent session list, click to expand visual step tree
4. **Pulsar** — live radar field showing every LLM call in real time (canvas-based, real data only)
5. **Warnings** — hallucination flags, RAG contradictions, claim density alerts, specific reason strings
6. **Alerts** — cost spike alerts
7. **Settings** — all 9 provider API keys, feature configuration, cross-model setup

### Gateway API Endpoints
- POST /v1/chat/completions — main proxy endpoint
- GET /health
- GET /metrics — Prometheus format, 8 ajah_ metrics
- GET /metrics/cost
- GET /metrics/traces
- GET /sessions
- GET /sessions/{sessionID}
- GET /warnings
- GET /warnings/{requestID}
- POST /settings
- GET /settings

---

## Test Coverage

**67 total tests — all passing**

- 62 unit tests across 11 packages
- 5 integration tests:
  - TestGatewayIntegration
  - TestAgentSessionIntegration
  - TestSessionReaping
  - TestHallucinationFlagging
  - TestRAGVerification
  - TestCrossModelVerification

**25/25 stress tests passing** (real LLM responses via Groq):
- Batch 1: Grounded requests 5/5
- Batch 2: Unsupported detection 5/5
- Batch 3: Contradiction detection 5/5
- Batch 4: Elite math (Riemann Hypothesis, Ramanujan series, Euler-Lagrange) 5/5
- Batch 5: Adversarial edge cases 5/5
- Average grounding score: 0.790
- False negative rate: 0.0%

---

## Repository Structure
ajah/
├── cmd/gateway/          — Go gateway entry point (main.go, handlers.go)
├── internal/
│   ├── proxy/            — HTTP reverse proxy, provider routing
│   ├── attribution/      — Cost calculation engine
│   ├── masking/          — PII detection and masking
│   ├── events/           — Async event emitter
│   ├── sessions/         — Agent session accumulator
│   ├── flagging/         — Hallucination risk flagger
│   ├── crossmodel/       — Cross-model verification
│   ├── metrics/          — Prometheus metrics registry
│   ├── storage/          — ClickHouse trace writer, session storage
│   ├── db/               — PostgreSQL settings store
│   └── config/           — Environment variable config
├── scorer/               — Python FastAPI quality scorer
│   ├── main.py
│   ├── scorer.py         — QualityScorer with RAG + claim density
│   ├── rag_verifier.py   — RAGVerifier using sentence-transformers
│   └── models.py         — Pydantic request/response models
├── dashboard/            — React TypeScript frontend
│   └── src/
│       ├── pages/        — Overview, Traces, Sessions, Pulsar, Warnings, Alerts, Settings
│       ├── components/   — Layout, sidebar
│       └── lib/          — pulsar-engine.js (canvas radar engine)
├── landing/              — useajah.com static HTML site
├── tests/                — End-to-end test scripts
│   ├── test_rag_real.ps1
│   ├── test_rag_real.py
│   └── stress_test_rag.ps1
└── examples/             — LangChain and CrewAI integration examples

---

## Docker Services
make up  →  starts all 6 containers:

| Service | Port | Status |
|---|---|---|
| gateway | 8080 | Go proxy |
| dashboard | 3000 | React UI |
| scorer | 8001 | Python ML |
| clickhouse | 8124, 9001 | Trace storage |
| postgres | 5433 | Settings |
| redis | 6380 | Metrics |

**Note:** Non-default ports due to collision with another project (wealthos) on this machine.

**DNS fix required:** docker-compose.yml has Google DNS added to gateway and scorer services (8.8.8.8, 8.8.4.4). Without this, containers cannot reach external LLM APIs on Windows.

---

## Environment Setup

Copy `.env.example` to `.env` and set:
- CLICKHOUSE_PASSWORD (set to: ajahprod — change in production)
- CLICKHOUSE_URL
- REDIS_URL
- DATABASE_URL

API keys are stored in PostgreSQL via the Settings page — NOT in .env

---

## Current Groq API Key Situation

- Previous key was accidentally exposed in git history — revoked
- New key set via Settings page at localhost:3000/settings
- Model to use: llama-3.3-70b-versatile (llama3-8b-8192 is decommissioned)

---

## Pricing

- **Self-Hosted:** Free forever, MIT license
- **Managed Cloud:** $199/month — dedicated instance, live in 24 hours, 14-day free trial
- **Enterprise:** Contact us — custom deployment, SLA, SSO, RBAC, audit logs
- **Contact:** vigneshreddy181200@gmail.com

---

## Social Media Presence

- **X/Twitter:** @ajah_io — posting daily at 9 AM IST
- **LinkedIn:** Posting daily
- **LangChain Slack:** Posted in #show-and-tell
- **LlamaIndex Discord:** Posted in show-and-tell channel
- **Dev.to:** Article published — "Why I built Ajah after Helicone went into maintenance mode"
- **Reddit r/buildinpublic:** Posting daily
- **Reddit r/startupaccelerator:** Posted
- **Product Hunt:** Submitted
- **Hacker News:** Account created, needs karma before Show HN allowed

---

## Daily Posting Schedule (IST)

Post every day at 9 AM IST.
See SOCIAL_HANDOFF.md for full angle library and posting rules.
Currently on: Day 3 complete, Day 4 pending.

---

## Commits Made (June 1, 2026)

- feat: claim density scoring — flags high-risk responses with many specific claims on low context
- fix: warnings list now includes medium-risk responses, add rag_verdict to warning items
- refactor: single scorer call, full result threaded through to Evaluate()
- docs: add pricing section to README
- feat: add Prometheus /metrics endpoint to gateway
- docs: social media posting handoff for dedicated posting chat

---

## Immediate Next Tasks (Priority Order)

1. Linguistic hedge detection — detects mismatch between question complexity and response certainty
2. Slack webhook for cost spikes
3. Get first paying customer — pricing live at $199/month, contact vigneshreddy181200@gmail.com

---

## Notable Interactions

- Dev.to: Founder from Moonshift replied asking about core signal vs Helicone. Replied with honest breakdown of what Ajah does and doesn't solve.
- Dev.to: Developer asked about latency benchmarks. Replied with <2ms overhead data.

---

## Unsolved Problems (Future Roadmap)

1. **Domain-specific hallucination detection** — Ajah cannot detect hallucinations in specialized domains (Vedic astrology, medicine, law) where ground truth is not computable.
2. **Confidence calibration** — Models don't know what they don't know. Confident wrong answers look identical to confident right answers in embeddings. Target signals: linguistic hedge detection, question complexity vs response certainty gap.
3. **Narrative drift detection** — Conversation-level analysis of when a model reverses a position under social pressure.
4. **Position shift detection** — Compare model claims in turn 3 vs turn 12. Significant reversal = flag.

---

## Market Positioning

**Primary target:** Enterprise teams in healthcare, finance, government who legally cannot use cloud-based tools.

**Entry wedge:** Helicone refugees — developers displaced when Helicone went into maintenance mode in March 2026.

**Key differentiators vs competitors:**
- Langfuse: observability only, no gateway, acquired by ClickHouse
- Portkey: gateway only, advanced features cloud-locked
- Helicone: acquired, maintenance mode
- LiteLLM: open source but compromised, no quality scoring

**Revenue model:** Open source core (free forever) + managed cloud hosting ($199/month) + enterprise support contracts

**Funding target:** $300K-500K pre-seed

---

## Important Technical Notes

- Scorer latency observed at ~9500ms on some requests — monitor as traffic scales
- Claim density threshold 0.6 is empirical, not scientifically derived — adjust after 2 weeks of real traces
- The Pulsar radar page uses a canvas engine (pulsar-engine.js). Real-data-only mode. Polling /metrics/traces every 2 seconds.
- Quality scores show 0.00 on failed requests (401, 502 etc.) — correct behavior.
- Windows DNS issue: Docker containers couldn't reach external APIs. Fixed by adding Google DNS (8.8.8.8) to docker-compose.yml.

---

## Co-Founder Note to New Claude Instance

Act as a senior technical co-founder. Be direct, honest, analytical. No flattery. Question assumptions. Point out blind spots.

The founder (Vignesh) is ambitious, technically capable, and building seriously. Push him toward users and distribution — he has a tendency to keep building features instead of getting the product in front of real people.

The product is real and working. The gap is zero paying users. Every conversation should push toward that goal.

When Vignesh wants to build a new feature, ask: does this remove friction for existing interested users, or does it add capability for users who haven't arrived yet? Build the former. Defer the latter until a real user asks for it.
