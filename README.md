<div align="center">

<br>

```
   ██████╗      ██╗ █████╗ ██╗  ██╗
  ██╔══██╗     ██║██╔══██╗██║  ██║
  ███████║     ██║███████║███████║
  ██╔══██║██   ██║██╔══██║██╔══██║
  ██║  ██║╚█████╔╝██║  ██║██║  ██║
  ╚═╝  ╚═╝ ╚════╝ ╚═╝  ╚═╝╚═╝  ╚═╝
```

### Open-Source LLM Observability Gateway

**Website:** https://useajah.com
**GitHub:** https://github.com/VigneshReddy-afk/ajah

*Intercept · Attribute · Mask · Score · Alert*

<br>

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev)
[![Python](https://img.shields.io/badge/Python-3.11-3776AB?style=for-the-badge&logo=python&logoColor=white)](scorer/)
[![React](https://img.shields.io/badge/React-19-61DAFB?style=for-the-badge&logo=react&logoColor=black)](dashboard/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=for-the-badge&logo=docker&logoColor=white)](docker-compose.yml)
[![License](https://img.shields.io/badge/License-MIT-22c55e?style=for-the-badge)](LICENSE)

<br>

**Point your app at `localhost:8080` instead of OpenAI.**
**Everything else — cost, PII, quality, alerts — is automatic.**

<br>

[**Quick Start**](#-quick-start) · [**Features**](#-features) · [**First Request**](#-your-first-request) · [**Architecture**](#-architecture) · [**Providers**](#-supported-providers)

<br>

</div>

---

## 🔥 Why Ajah?

> Every LLM observability tool is now cloud-locked, acquired, or sends your prompts to someone else's server.

<table>
<tr>
<td width="33%">

**☁️ The Problem**

Datadog AI, LangSmith, Helicone — all SaaS. Your prompts leave your network. Not an option for regulated industries.

</td>
<td width="33%">

**🔒 The Gap**

No single self-hosted tool combines gateway + cost attribution + PII masking + hallucination scoring + alerting.

</td>
<td width="33%">

**⚡ Ajah**

One `docker-compose up`. Your traffic stays on your server. Full observability stack in under 5 minutes.

</td>
</tr>
</table>

---

## ✨ Features

<table>
<tr>
<td width="50%">

### 🌐 9-Provider Gateway
Route to OpenAI, Anthropic, Groq, Gemini, Grok, Mistral, Together, NVIDIA, or Cohere — detected automatically from key prefix. Zero config.

</td>
<td width="50%">

### 💰 Real-Time Cost Attribution
Per-user, per-feature, per-model cost tracking. Daily spend counters in Redis. Spike alerts fire the moment a threshold is crossed.

</td>
</tr>
<tr>
<td width="50%">

### 🛡️ PII Detection & Masking
Emails, phones, credit cards, SSNs, API keys — detected via regex before storage. Originals never hit ClickHouse.

</td>
<td width="50%">

### 🧠 Local ML Quality Scoring
`sentence-transformers` + `toxic-bert` run entirely on your hardware. Hallucination risk, factual consistency, and toxicity — scored on every response.

</td>
</tr>
<tr>
<td width="50%">

### ⚠️ Hallucination Flagging
High-risk responses (hallucination > 0.7 or grounding < 0.3) are flagged, stored, and surfaced in the Warnings dashboard. Webhook delivery per feature.

</td>
<td width="50%">

### 📊 Observability Dashboard
Live traces, session step-explorer, cost charts, warning feed, and alert history — all in a self-hosted React dashboard at `:3000`.

</td>
</tr>
</table>

---

## 🚀 Quick Start

```bash
git clone https://github.com/VigneshReddy-afk/ajah
cd ajah
cp .env.example .env        # add your ClickHouse password
docker-compose up -d
```

> [!TIP]
> Dashboard → **http://localhost:3000**
> Gateway → **http://localhost:8080**
>
> **Note:** `localhost` refers to the server where you run `docker-compose`. Replace with your server's IP or domain if deploying remotely.

That's it. All services start together: Gateway, Scorer, ClickHouse, Redis, PostgreSQL, Dashboard.

---

## ⚡ Your First Request

Drop in one line change — swap `base_url` and you're traced.

<details open>
<summary><b>Python</b></summary>

```python
import openai

client = openai.OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="your-openai-key"           # your real key, unchanged
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello"}],
    extra_headers={
        "X-User-ID":      "user_1",     # tracked in cost breakdown
        "X-Feature-Name": "chat"        # grouped in the dashboard
    }
)
```

</details>

<details>
<summary><b>Node.js</b></summary>

```javascript
import OpenAI from 'openai';

const client = new OpenAI({
    baseURL: 'http://localhost:8080/v1',
    apiKey:  process.env.OPENAI_API_KEY,
    defaultHeaders: {
        'X-User-ID':      'user_1',
        'X-Feature-Name': 'chat'
    }
});
```

</details>

<details>
<summary><b>cURL</b></summary>

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -H "X-User-ID: user_1" \
  -H "X-Feature-Name: chat" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

</details>

> [!NOTE]
> Works with **any** of the 9 supported providers — Ajah detects the provider from your key prefix automatically. Swap `gsk_...` for Groq, `sk-ant-...` for Anthropic, etc.

After sending — open **http://localhost:3000** to see your cost, quality score, hallucination risk, and full trace.

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Your Application                      │
└───────────────────────────┬─────────────────────────────────┘
                            │  HTTP  (Authorization: Bearer sk-...)
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                      Ajah Gateway  :8080                     │
│                                                              │
│   ┌──────────────┐    ┌──────────────────────────────────┐  │
│   │ Proxy Router │───▶│         LLM Provider API         │  │
│   │  (Go/chi)    │◀───│  OpenAI · Groq · Anthropic · ... │  │
│   └──────┬───────┘    └──────────────────────────────────┘  │
│          │ fire-and-forget (zero added latency)              │
│          ▼                                                   │
│   ┌──────────────────────────────────────────────────────┐  │
│   │                  Async Pipeline                       │  │
│   │  PII Mask → Cost → Quality Score → Hallucination Flag │  │
│   └────┬──────────────┬────────────────────┬─────────────┘  │
└────────│──────────────│────────────────────│────────────────┘
         │              │                    │
         ▼              ▼                    ▼
   ┌──────────┐  ┌────────────┐     ┌──────────────┐
   │ClickHouse│  │   Redis    │     │  PostgreSQL  │
   │  Traces  │  │  Metrics   │     │   Settings   │
   │ Sessions │  │  Counters  │     │  API Keys    │
   └──────────┘  │  Alerts    │     └──────────────┘
                 │  Warnings  │
                 └────────────┘
                       │
                       ▼
            ┌─────────────────────┐
            │  Dashboard  :3000   │
            │  React · TypeScript │
            └─────────────────────┘
```

<br>

<table>
<thead>
<tr>
<th>Component</th>
<th>Stack</th>
<th>Key detail</th>
</tr>
</thead>
<tbody>
<tr>
<td>🔀 <b>Gateway Proxy</b></td>
<td>Go · chi · net/http</td>
<td>&lt;2 ms added latency — async pipeline never blocks the response</td>
</tr>
<tr>
<td>⚙️ <b>Async Pipeline</b></td>
<td>Go workers · channels</td>
<td>Configurable concurrency, graceful drain on shutdown</td>
</tr>
<tr>
<td>🧪 <b>Quality Scorer</b></td>
<td>Python · FastAPI · sentence-transformers · toxic-bert</td>
<td>Runs locally — no data sent to external ML APIs</td>
</tr>
<tr>
<td>🗄️ <b>Storage</b></td>
<td>ClickHouse · Redis · PostgreSQL</td>
<td>Traces in CH, hot counters in Redis, settings in PG</td>
</tr>
<tr>
<td>📺 <b>Dashboard</b></td>
<td>React 19 · TypeScript · Recharts</td>
<td>Traces, sessions, warnings, alerts, settings — all live</td>
</tr>
</tbody>
</table>

---

## 🔌 Supported Providers

<div align="center">

[![OpenAI](https://img.shields.io/badge/OpenAI-sk--...-00A67E?style=for-the-badge&logo=openai&logoColor=white)](https://openai.com)
[![Anthropic](https://img.shields.io/badge/Anthropic-sk--ant--...-D97757?style=for-the-badge)](https://anthropic.com)
[![Groq](https://img.shields.io/badge/Groq-gsk__...-F55036?style=for-the-badge)](https://groq.com)
[![Gemini](https://img.shields.io/badge/Google_Gemini-AIza...-4285F4?style=for-the-badge&logo=google&logoColor=white)](https://ai.google.dev)
[![xAI](https://img.shields.io/badge/xAI_Grok-xai--...-000000?style=for-the-badge)](https://x.ai)
[![Mistral](https://img.shields.io/badge/Mistral-mistral--...-FF7000?style=for-the-badge)](https://mistral.ai)
[![Together](https://img.shields.io/badge/Together_AI-together--...-6C47FF?style=for-the-badge)](https://together.ai)
[![NVIDIA](https://img.shields.io/badge/NVIDIA-nvapi--...-76B900?style=for-the-badge&logo=nvidia&logoColor=white)](https://build.nvidia.com)
[![Cohere](https://img.shields.io/badge/Cohere-cohere--...-39594E?style=for-the-badge)](https://cohere.com)

</div>

> [!IMPORTANT]
> Provider is **auto-detected from the key prefix** — no routing config, no YAML, no environment variable per provider. Pass your key, Ajah figures out the rest.

---

## 📡 Dashboard Pages

| Page | What you see |
|---|---|
| **Overview** | Cost by feature · Cost by model (donut) · Quality trend line |
| **Traces** | Live feed of every LLM call — cost, latency, quality, PII flag |
| **Sessions** | Multi-step agent sessions with visual step-flow explorer |
| **Warnings** | Flagged responses — hallucination risk, grounding score, reasons |
| **Alerts** | Cost-spike alerts with threshold vs. actual |
| **Settings** | Provider API keys · Feature budgets · PII toggle · Webhook URLs |

---

## 🧩 Optional Headers

| Header | Purpose |
|---|---|
| `X-User-ID` | Attribute cost to a user |
| `X-Feature-Name` | Group traces by feature (chat, summarize, classify…) |
| `X-Session-ID` | Link multiple calls into an agent session |
| `X-Agent-Step` | Label the step inside a session |
| `X-Parent-Step-ID` | Build the step tree for the visual explorer |
| `X-Request-ID` | Pass your own trace ID (returned in `X-Ajah-Request-ID`) |
| `X-Source-Context` | Base64-encoded source document for RAG verification |

All headers are optional. Omit them and Ajah still traces everything.

---

## 🔍 RAG Verification

When your application uses retrieval-augmented generation (RAG), Ajah can verify whether the LLM response is actually grounded in your source documents.

Pass your source context in the request header:

```python
import base64

source_doc = "Your policy document text here..."
encoded = base64.b64encode(source_doc.encode()).decode()

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": question}],
    extra_headers={
        "X-User-ID":        "user_1",
        "X-Feature-Name":   "support-bot",
        "X-Source-Context": encoded        # base64-encoded source document
    }
)
```

Ajah verifies each claim in the response against your source document using local sentence embeddings (`all-MiniLM-L6-v2`) and returns one of four verdicts:

| Verdict | Meaning | Risk impact |
|---|---|---|
| `supported` | All claims grounded in source | None |
| `partially_supported` | Some claims unverified | Medium |
| `unsupported` | Claims not found in source | Medium |
| `contradicted` | Response contradicts source | **Forced high** |

`contradicted` responses are automatically upgraded to **high risk** in the Warnings dashboard, regardless of the hallucination score. All verdicts, grounding scores, and per-claim breakdowns are stored in ClickHouse and visible in the Traces page expanded row.

> [!NOTE]
> The source document is decoded and processed entirely on your server by the local scorer — it is never forwarded to the LLM provider. The `X-Source-Context` header is stripped before the request leaves the gateway.

---

## 💼 Pricing

<table>
<tr>
<td width="33%">

### 🆓 Self-Hosted
**Free forever**

Full open source, MIT license.
You run it, you own it.
No limits, no restrictions.
Community support via GitHub Issues.

[**Get Started →**](#-quick-start)

</td>
<td width="33%">

### ☁️ Managed Cloud
**$199 / month**

We run it for you on dedicated
infrastructure. Your data never
leaves your instance.

- Live in 24 hours
- Email support included
- 14-day free trial

[**Email us →**](mailto:vigneshreddy181200@gmail.com)

</td>
<td width="33%">

### 🏢 Enterprise
**Contact us**

Custom deployment on your
infrastructure with SLA guarantee.

- Priority support
- SSO + RBAC + Audit logs
- Compliance documentation
- Custom feature development

[**Contact →**](mailto:vigneshreddy181200@gmail.com)

</td>
</tr>
</table>

> Questions or want managed hosting?
> Email **vigneshreddy181200@gmail.com** —
> we'll have you running within 24 hours.

---

## 🔗 Links

- **Website:** https://useajah.com
- **GitHub:** https://github.com/VigneshReddy-afk/ajah
- **Issues:** https://github.com/VigneshReddy-afk/ajah/issues
- **Discord:** https://discord.gg/JktkwHbWx

---

## 🤝 Contributing

Ajah is open source under the MIT license. PRs welcome.

---

<div align="center">

**Built with Go · Python · React · ClickHouse · Redis · PostgreSQL**

[![MIT License](https://img.shields.io/badge/License-MIT-22c55e?style=flat-square)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-185FA5?style=flat-square)](https://github.com/VigneshReddy-afk/ajah/pulls)

*No data leaves your server. Ever.*

</div>
