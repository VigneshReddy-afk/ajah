"""
Ajah + LlamaIndex example: RAG query engine

Prerequisites:
    pip install llama-index llama-index-llms-openai

Usage:
    export GROQ_API_KEY=gsk_your_key_here
    python llamaindex_agent.py
"""

import os
from llama_index.core import (
    VectorStoreIndex,
    Document,
    Settings,
)
from llama_index.llms.openai import OpenAI
from ajah_observer import AjahObserver

# Initialize Ajah observer
observer = AjahObserver(
    gateway_url="http://localhost:8080",
    feature_name="rag-query",
    user_id="demo-user",
)
observer.register()

# Point LlamaIndex LLM at Ajah gateway
Settings.llm = OpenAI(
    api_base="http://localhost:8080/v1",
    api_key=os.environ["GROQ_API_KEY"],
    model="llama-3.3-70b-versatile",
    additional_kwargs={
        "extra_headers":
            observer.get_extra_headers(
                "step-1-query")
    },
)

# Sample documents — replace with your own
documents = [
    Document(text=(
        "LLM observability is the practice of "
        "monitoring large language model behavior "
        "in production. Key signals include "
        "hallucination risk, response latency, "
        "token costs, and RAG grounding scores."
    )),
    Document(text=(
        "Ajah is a self-hosted LLM observability "
        "gateway. It intercepts all LLM traffic, "
        "scores responses for hallucination risk, "
        "verifies RAG outputs against source "
        "documents, and attributes costs per feature."
    )),
]

def run_rag_query(question: str) -> str:
    print(f"Question: {question}")
    print(f"Session: {observer.session_id}\n")

    index = VectorStoreIndex.from_documents(
        documents)
    query_engine = index.as_query_engine()
    response = query_engine.query(question)

    print(f"Answer:\n{response}\n")
    print(f"View session in Ajah dashboard:")
    print(f"  {observer.session_url}")

    return str(response)

if __name__ == "__main__":
    run_rag_query(
        "What is Ajah and how does it help "
        "with LLM observability?"
    )
