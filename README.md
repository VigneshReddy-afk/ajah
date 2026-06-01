# Ajah — LLM Observability Gateway

> The observatory at the edge of your AI universe.
> Nothing escapes undetected.

Ajah sits between your application and every LLM
provider. It sees everything. It stops the bad
stuff before it reaches your users.

Self-hostable. No data leaves your server.
No vendor lock-in. No acquisition risk.

---

## Why Ajah

Helicone got acquired. Langfuse got acquired.

Every cloud-based observability tool has the same
fatal flaw — your prompts leave your server.

For teams in healthcare, finance, and government,
that's not a trade-off. That's a blocker.

Ajah runs entirely on your infrastructure.
One command. Full control.

---

## What It Does

**Gateway Proxy**
Point your app at Ajah instead of your LLM provider.
One line change. Supports 9 providers automatically.
Less than 2ms overhead.

**RAG Verification**
Verifies LLM responses against your source documents.
Catches contradictions before they reach your users.

**Hallucination Flagging**
Every response scored in parallel. Zero latency added.
Local ML models. No external API calls.

**Claim Density Scoring**
Flags responses that make many specific claims
on low-context prompts — a class of hallucination
risk that embedding similarity misses.

**PII Masking**
EMAIL, PHONE, SSN, CREDIT_CARD, IP_ADDRESS, API_KEY
Masked before anything is stored. Compliance built in.

**Cost Attribution**
Track LLM spend by user, feature, and model.
Know exactly what each part of your product costs.

**Multi-Agent Session Tracing**
Visual step-by-step trace of every agent run.
Cost, quality, and latency per step.

**Cross-Model Verification**
Send the same prompt to a second model.
Flag disagreements before they reach production.

---

## Supported Providers

OpenAI · Anthropic · Groq · Gemini · Grok ·
Mistral · Together · NVIDIA · Cohere

Auto-detected from your API key prefix.
No configuration needed.

---

## Quick Start

```bash
git clone https://github.com/VigneshReddy-afk/ajah
cd ajah
cp .env.example .env
docker-compose up
```

Dashboard live at http://localhost:3000

---

## Test Results

- 67 unit and integration tests — all passing
- 25/25 stress tests passing with real LLM responses
- Tested against Riemann Hypothesis, Ramanujan series,
  Euler-Lagrange equations, and adversarial edge cases
- False negative rate: 0.0%
- Average grounding score: 0.790

---

## Tech Stack

| Component | Technology |
|---|---|
| Gateway | Go 1.22 |
| Quality Scorer | Python 3.11 + FastAPI |
| ML Models | sentence-transformers, toxic-bert |
| Trace Storage | ClickHouse |
| Real-time Metrics | Redis |
| Config | PostgreSQL |
| Dashboard | React 19 + TypeScript |
| Deployment | Docker Compose |

---

## Pricing

### Self-Hosted — Free Forever
- Full open source, MIT license
- You run it, you own it
- Community support via GitHub Issues
- No limits, no restrictions

### Managed Cloud — $199/month
- We run it for you on dedicated infrastructure
- Your data never leaves your instance
- Live in 24 hours
- Email support included
- 14-day free trial

### Enterprise — Contact Us
- Custom deployment on your infrastructure
- SLA guarantee
- Priority support
- SSO, RBAC, audit logs
- Compliance documentation

---

## Get Started

**Self-hosted:** Clone the repo and run
`docker-compose up` — you're live in minutes.

**Managed hosting or enterprise:**
Email vigneshreddy181200@gmail.com

We'll have you running within 24 hours.

---

## Links

- Website: https://useajah.com
- GitHub: https://github.com/VigneshReddy-afk/ajah

---

## License

MIT — free forever for self-hosted use.
