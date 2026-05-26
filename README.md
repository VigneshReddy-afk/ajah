# ajah

Self-hostable LLM observability platform. Intercepts LLM API traffic, attributes costs, masks PII, and scores output quality — without adding meaningful latency.

## Quick start

```bash
docker-compose up
```

## Architecture

- **Gateway Proxy** (Go) — HTTP proxy with <2ms overhead
- **Async Pipeline** (Go + Python) — cost attribution, PII masking, quality scoring
- **Storage** — Redis, PostgreSQL, ClickHouse
- **Dashboard** — React + TypeScript
