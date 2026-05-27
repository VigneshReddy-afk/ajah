"""
CrewAI + Ajah example: 3-agent crew with full session tracing.

A single session ID is shared across all agents in the crew run so every
LLM call appears as a linked step in the Ajah Sessions dashboard.

CrewAI uses LangChain under the hood, so we monkey-patch the OpenAI client
at startup to route through the Ajah gateway and inject the session headers.
"""

import uuid
import functools
from openai import OpenAI
from crewai import Agent, Task, Crew, Process
from crewai import LLM

# ── Session identity ───────────────────────────────────────────────────────
session_id = str(uuid.uuid4())
USER_ID    = "demo-user"
FEATURE    = "research-crew"

print(f"Starting crew run — Session ID: {session_id}")

# ── Ajah-aware LLM factory ─────────────────────────────────────────────────
# CrewAI's LLM wrapper accepts extra_headers that are forwarded on every call.
# We create one LLM config per agent so each step carries a distinct step name.

GROQ_KEY = "gsk_YOUR_GROQ_KEY_HERE"  # replace with your key
MODEL     = "groq/llama-3.3-70b-versatile"


def ajah_llm(step_name: str) -> LLM:
    return LLM(
        model=MODEL,
        base_url="http://localhost:8080/v1",
        api_key=GROQ_KEY,
        extra_headers={
            "X-Session-ID":   session_id,
            "X-Agent-Step":   step_name,
            "X-Feature-Name": FEATURE,
            "X-User-ID":      USER_ID,
        },
    )


# ── Agents ─────────────────────────────────────────────────────────────────

researcher = Agent(
    role="Senior Research Analyst",
    goal="Find comprehensive, accurate information on the given topic.",
    backstory=(
        "You are an expert researcher with a talent for finding and evaluating "
        "sources. You produce thorough, fact-based research summaries."
    ),
    llm=ajah_llm("step-1-researcher"),
    verbose=True,
)

writer = Agent(
    role="Technical Writer",
    goal="Transform research findings into a clear, engaging report.",
    backstory=(
        "You are a skilled technical writer who can take complex research and "
        "distill it into accessible, well-structured prose."
    ),
    llm=ajah_llm("step-2-writer"),
    verbose=True,
)

reviewer = Agent(
    role="Quality Reviewer",
    goal="Review the report for accuracy, clarity, and completeness.",
    backstory=(
        "You are a meticulous editor with a sharp eye for factual errors, "
        "logical gaps, and unclear writing. You provide actionable feedback."
    ),
    llm=ajah_llm("step-3-reviewer"),
    verbose=True,
)

# ── Tasks ──────────────────────────────────────────────────────────────────

research_task = Task(
    description=(
        "Research the topic: {topic}\n\n"
        "Produce a structured summary covering:\n"
        "1. Key concepts and definitions\n"
        "2. Current state of the field\n"
        "3. Main challenges and open problems\n"
        "4. Notable tools, frameworks, or approaches"
    ),
    expected_output="A structured research summary with 4 clearly labelled sections.",
    agent=researcher,
)

writing_task = Task(
    description=(
        "Using the research summary provided, write a concise 3-paragraph report "
        "on: {topic}\n\n"
        "The report should be suitable for a technical audience but avoid jargon "
        "where possible. Include a one-sentence executive summary at the top."
    ),
    expected_output="A polished 3-paragraph technical report with an executive summary.",
    agent=writer,
    context=[research_task],
)

review_task = Task(
    description=(
        "Review the report on: {topic}\n\n"
        "Check for:\n"
        "- Factual accuracy against the research findings\n"
        "- Clarity and logical flow\n"
        "- Completeness (are key points missing?)\n\n"
        "Output the final revised report with any corrections applied."
    ),
    expected_output="The final, reviewed and corrected report ready for publication.",
    agent=reviewer,
    context=[research_task, writing_task],
)

# ── Crew ───────────────────────────────────────────────────────────────────

crew = Crew(
    agents=[researcher, writer, reviewer],
    tasks=[research_task, writing_task, review_task],
    process=Process.sequential,
    verbose=True,
)

# ── Run ────────────────────────────────────────────────────────────────────

if __name__ == "__main__":
    topic = "LLM observability: monitoring and debugging large language model applications in production"

    print(f"\n{'='*60}")
    print(f"CrewAI + Ajah Research Crew")
    print(f"Session ID : {session_id}")
    print(f"Topic      : {topic}")
    print(f"{'='*60}\n")

    result = crew.kickoff(inputs={"topic": topic})

    print(f"\n{'='*60}")
    print("Final Report:")
    print(f"{'='*60}")
    print(result)
    print(f"\n{'='*60}")
    print(f"Crew run complete.")
    print(f"Session ID : {session_id}")
    print(f"Dashboard  : http://localhost:3000/sessions")
    print(f"{'='*60}\n")
