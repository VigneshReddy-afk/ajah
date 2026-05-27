# Ajah Examples

Drop-in examples showing how to add Ajah session tracing to agent frameworks.
Each example routes LLM calls through the Ajah gateway so cost, latency,
quality score, and step metadata are recorded automatically.

## Prerequisites

- Python 3.11+
- Ajah running locally (`docker-compose up -d` from the repo root)
- A provider API key (examples use Groq — free tier at console.groq.com)

## Setup

```bash
# LangChain example
pip install openai

# CrewAI example
pip install openai crewai
```

Edit the example file and replace `gsk_YOUR_GROQ_KEY_HERE` with your actual
Groq API key before running.

---

## langchain_example.py

A minimal 3-step research agent built directly with the OpenAI SDK (no
LangChain dependency needed — the pattern works with any framework that
accepts `extra_headers`).

```bash
python examples/langchain_example.py
```

**What happens:**

1. A UUID session ID is generated for the run
2. Three sequential LLM calls are made — `step-1-planner`, `step-2-researcher`,
   `step-3-synthesizer` — each carrying the session ID in `X-Session-ID`
3. The Ajah gateway records each call: tokens, cost, latency, quality score
4. After ~5 minutes idle the session reaper flushes the session to ClickHouse

**What you'll see in the dashboard** (`http://localhost:3000/sessions`):

- A new row appears with `feature_name: research-agent`, `step_count: 3`
- Clicking the row expands the step flow diagram: three cards left to right,
  each showing model, cost, latency, and quality score
- Quality score cards are colour-coded (green ≥ 0.8, amber 0.5–0.8, red < 0.5)

---

## crewai_example.py

A 3-agent CrewAI crew — researcher → writer → reviewer — sharing one session ID.

```bash
python examples/crewai_example.py
```

**What happens:**

1. A UUID session ID is generated before the crew starts
2. Each agent is given a `crewai.LLM` configured with `base_url` pointing at
   Ajah and `extra_headers` carrying `X-Session-ID` and a distinct `X-Agent-Step`
3. CrewAI runs the tasks sequentially; each agent's LLM calls flow through Ajah
4. All calls share the same session ID, so the entire crew run is one session

**What you'll see in the dashboard:**

- One session row with `step_count: 3` (one per agent)
- Step flow: `step-1-researcher → step-2-writer → step-3-reviewer`
- Because no `X-Parent-Step-ID` is set, all three appear as root nodes in a
  linear left-to-right flow

---

## Key headers

| Header | Purpose | Example |
|---|---|---|
| `X-Session-ID` | Groups all steps into one agent run | `uuid4()` |
| `X-Agent-Step` | Names this step in the session timeline | `"step-1-planner"` |
| `X-Feature-Name` | Tags which product feature triggered the run | `"research-agent"` |
| `X-User-ID` | Associates cost and usage with a user | `"user_42"` |
| `X-Parent-Step-ID` | Marks a step as a child branch of another | `"step-1-planner"` |
| `X-Tool-Name` | Records which tool this step invoked | `"web_search"` |

Headers are optional — omitting any header simply leaves that field blank in
the trace. `X-Session-ID` is the one that enables session grouping.
