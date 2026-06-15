# Ajah Python SDK

Python SDK for [Ajah](https://useajah.com) —
self-hosted LLM observability gateway.

## Installation

pip install ajah-sdk

## Quick Start

from ajah import AjahClient

client = AjahClient(
    gateway_url="http://localhost:8080",
    api_key="your-groq-key",
    feature_name="my-app",
    user_id="user-123",
)

response = client.chat(
    model="llama-3.3-70b-versatile",
    messages=[{"role": "user",
               "content": "Hello"}],
)
print(response.choices[0].message.content)

## Session Tracking

with client.session() as session:
    response1 = session.chat(
        model="llama-3.3-70b-versatile",
        messages=[{"role": "user",
                   "content": "Plan a trip"}],
        step_name="step-1-planner",
    )
    response2 = session.chat(
        model="llama-3.3-70b-versatile",
        messages=[{"role": "user",
                   "content": "Book flights"}],
        step_name="step-2-booker",
    )
    print(f"Session dashboard: {session.dashboard_url}")

## Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| gateway_url | Ajah gateway URL | http://localhost:8080 |
| api_key | Your LLM provider API key | required |
| feature_name | Feature name for cost attribution | "default" |
| user_id | User ID for cost tracking | "anonymous" |

## What You Get

- Cost attribution per feature and model
- Hallucination risk scoring on every response
- RAG verification against source documents
- Narrative drift detection across sessions
- PII masking before storage
- Full session traces in the dashboard

## Self-Hosted

git clone https://github.com/VigneshReddy-afk/ajah
cd ajah
docker-compose up -d

MIT License · GitHub · useajah.com
