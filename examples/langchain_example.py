"""
LangChain + Ajah example: 3-step research agent with full session tracing.

Every LLM call passes Ajah headers so the gateway records cost, latency,
quality score, and step metadata. All three steps share the same session ID,
so they appear as one agent run in the Sessions dashboard.
"""

import uuid
from openai import OpenAI

# ── Ajah gateway client ────────────────────────────────────────────────────
# Point the OpenAI SDK at the local Ajah gateway.
# Any OpenAI-compatible provider key works (groq, openai, anthropic, etc.)
client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="gsk_YOUR_GROQ_KEY_HERE",  # replace with your key
)

MODEL = "llama-3.3-70b-versatile"

# ── Session identity ───────────────────────────────────────────────────────
# One UUID ties all steps of this agent run together in Ajah.
session_id = str(uuid.uuid4())
USER_ID = "demo-user"
FEATURE = "research-agent"


def call(step_name: str, messages: list[dict]) -> str:
    """Make a traced LLM call with Ajah session headers."""
    response = client.chat.completions.create(
        model=MODEL,
        messages=messages,
        extra_headers={
            "X-Session-ID":   session_id,
            "X-Agent-Step":   step_name,
            "X-Feature-Name": FEATURE,
            "X-User-ID":      USER_ID,
        },
    )
    return response.choices[0].message.content


# ── Step 1: Planner ────────────────────────────────────────────────────────
def plan(question: str) -> str:
    print(f"[step-1-planner] Planning research for: {question!r}")
    result = call(
        "step-1-planner",
        [
            {"role": "system", "content": "You are a research planner. Given a question, output a numbered list of 3 concrete research tasks needed to answer it. Be concise."},
            {"role": "user",   "content": question},
        ],
    )
    print(f"[step-1-planner] Plan:\n{result}\n")
    return result


# ── Step 2: Researcher ─────────────────────────────────────────────────────
def research(plan_text: str) -> str:
    print("[step-2-researcher] Executing research tasks...")
    result = call(
        "step-2-researcher",
        [
            {"role": "system", "content": "You are a thorough researcher. Given a list of research tasks, execute each one and provide detailed findings for each task."},
            {"role": "user",   "content": plan_text},
        ],
    )
    print(f"[step-2-researcher] Findings:\n{result[:400]}...\n")
    return result


# ── Step 3: Synthesizer ────────────────────────────────────────────────────
def synthesize(question: str, findings: str) -> str:
    print("[step-3-synthesizer] Synthesizing final answer...")
    result = call(
        "step-3-synthesizer",
        [
            {"role": "system", "content": "You are an expert writer. Synthesize the provided research findings into a clear, concise answer to the original question. Use 2-3 paragraphs."},
            {"role": "user",   "content": f"Original question: {question}\n\nResearch findings:\n{findings}"},
        ],
    )
    print(f"[step-3-synthesizer] Answer:\n{result}\n")
    return result


# ── Main ───────────────────────────────────────────────────────────────────
def run_agent(question: str) -> str:
    print(f"\n{'='*60}")
    print(f"Ajah Research Agent")
    print(f"Session ID : {session_id}")
    print(f"Question   : {question}")
    print(f"{'='*60}\n")

    research_plan = plan(question)
    findings      = research(research_plan)
    answer        = synthesize(question, findings)

    print(f"{'='*60}")
    print(f"Agent run complete.")
    print(f"Session ID : {session_id}")
    print(f"Dashboard  : http://localhost:3000/sessions")
    print(f"{'='*60}\n")

    return answer


if __name__ == "__main__":
    run_agent("What are the key challenges in LLM observability and how are they being solved?")
