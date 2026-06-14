"""
Ajah + LangChain example: 3-step research agent

Prerequisites:
    pip install langchain-openai langchain-core

Usage:
    export GROQ_API_KEY=gsk_your_key_here
    python langchain_agent.py
"""

import os
from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage, SystemMessage
from ajah_callback import AjahCallbackHandler

# Initialize Ajah handler — one session
# tracks all steps automatically
handler = AjahCallbackHandler(
    gateway_url="http://localhost:8080",
    feature_name="research-agent",
    user_id="demo-user",
)

def make_llm(step_name: str) -> ChatOpenAI:
    """Create an LLM instance for a specific step."""
    return ChatOpenAI(
        base_url="http://localhost:8080/v1",
        api_key=os.environ["GROQ_API_KEY"],
        model="llama-3.3-70b-versatile",
        callbacks=[handler],
        model_kwargs={
            "extra_headers":
                handler.get_extra_headers(step_name)
        },
    )

def plan(topic: str) -> str:
    llm = make_llm("step-1-planner")
    response = llm.invoke([
        SystemMessage(content=(
            "You are a research planner. "
            "Create a brief research plan."
        )),
        HumanMessage(content=f"Plan research on: {topic}"),
    ])
    return response.content

def research(plan: str) -> str:
    llm = make_llm("step-2-researcher")
    response = llm.invoke([
        SystemMessage(content=(
            "You are a researcher. "
            "Execute this research plan concisely."
        )),
        HumanMessage(content=plan),
    ])
    return response.content

def synthesize(research: str) -> str:
    llm = make_llm("step-3-synthesizer")
    response = llm.invoke([
        SystemMessage(content=(
            "You are a synthesizer. "
            "Create a clear summary."
        )),
        HumanMessage(content=research),
    ])
    return response.content

def run_agent(topic: str) -> str:
    print(f"Starting research on: {topic}")
    print(f"Session: {handler.session_id}\n")

    research_plan = plan(topic)
    print(f"Plan:\n{research_plan}\n")

    research_findings = research(research_plan)
    print(f"Research:\n{research_findings}\n")

    final_report = synthesize(research_findings)
    print(f"Report:\n{final_report}\n")

    print(f"View session in Ajah dashboard:")
    print(f"  {handler.session_url}")

    return final_report

if __name__ == "__main__":
    run_agent(
        "How does LLM observability improve "
        "AI reliability in production systems?"
    )
