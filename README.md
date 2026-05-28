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

## Your first request

Replace your existing OpenAI base URL with Ajah:

```python
import openai

client = openai.OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="your-openai-key"  # your real key
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello"}],
    extra_headers={
        "X-User-ID": "user_1",
        "X-Feature-Name": "chat"
    }
)
```

Works with any provider — just use the matching key prefix and Ajah routes automatically.
Then open http://localhost:3000 to see your cost, quality score, and trace.

```javascript
import OpenAI from 'openai';

const client = new OpenAI({
    baseURL: 'http://localhost:8080/v1',
    apiKey: process.env.OPENAI_API_KEY,
    defaultHeaders: {
        'X-User-ID': 'user_1',
        'X-Feature-Name': 'chat'
    }
});
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
