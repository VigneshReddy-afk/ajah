# Ajah — Open Source LLM Observability

Self-hostable gateway that intercepts LLM traffic, attributes costs, masks PII, and scores output quality. No data leaves your server.

---

## Why Ajah

- Every LLM observability tool is now cloud-locked or acquired
- Enterprises cannot send prompts to third-party servers
- No single tool combines gateway + cost attribution + PII masking + quality scoring

---

## Features

- **9 providers supported** — OpenAI, Anthropic, Groq, Gemini, Grok, Mistral, Together, NVIDIA, Cohere
- **Real-time cost attribution** by user, feature, and model
- **PII detection and masking** before storage
- **Local ML-based output quality scoring** — hallucination, factual consistency, toxicity
- **Full audit trail** in ClickHouse
- **Single `docker-compose up` deployment**

---

## Quick Start

```bash
git clone https://github.com/VigneshReddy-afk/ajah
cd ajah
cp .env.example .env
docker-compose up -d
# Dashboard at http://localhost:3000
# Gateway at http://localhost:8080
```

---

## How It Works

1. Point your app at `http://localhost:8080` instead of the LLM provider directly
2. Pass your API key in the `Authorization` header as normal
3. Ajah intercepts, routes, scores, and stores everything automatically

---

## Architecture

```
Your App → Ajah Gateway → LLM Provider
                ↓
         Async Pipeline
      (Cost | PII | Quality)
                ↓
          ClickHouse + Redis
                ↓
          Dashboard :3000
```

| Component | Stack |
|---|---|
| Gateway Proxy | Go — HTTP reverse proxy, <2ms overhead |
| Async Pipeline | Go workers + Python scorer (FastAPI) |
| Quality Scorer | sentence-transformers, toxic-bert |
| Storage | ClickHouse (traces), Redis (metrics), PostgreSQL (settings) |
| Dashboard | React 19, TypeScript, Recharts, TailwindCSS |

---

## Supported Providers

| Provider | Key Prefix |
|---|---|
| OpenAI | `sk-` |
| Anthropic | `sk-ant-` |
| Groq | `gsk_` |
| Google Gemini | `AIza` |
| xAI / Grok | `xai-` |
| Mistral | `mistral-` |
| Together AI | `together-` |
| NVIDIA | `nvapi-` |
| Cohere | `cohere-` |

Provider is detected automatically from the key prefix — no configuration required.

---

## Contributing

Ajah is open source under the MIT license. PRs welcome.

---

## License

MIT
