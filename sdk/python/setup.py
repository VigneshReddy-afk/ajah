from setuptools import setup, find_packages

setup(
    name="ajah-sdk",
    version="0.1.0",
    description=(
        "Python SDK for Ajah — "
        "self-hosted LLM observability gateway"
    ),
    long_description=open("README.md").read(),
    long_description_content_type="text/markdown",
    author="Vignesh Reddy",
    author_email="vigneshreddy181200@gmail.com",
    url="https://github.com/VigneshReddy-afk/ajah",
    packages=find_packages(),
    python_requires=">=3.9",
    install_requires=[
        "openai>=1.0.0",
        "httpx>=0.24.0",
    ],
    classifiers=[
        "Development Status :: 3 - Alpha",
        "Intended Audience :: Developers",
        "License :: OSI Approved :: MIT License",
        "Programming Language :: Python :: 3",
        "Programming Language :: Python :: 3.9",
        "Programming Language :: Python :: 3.10",
        "Programming Language :: Python :: 3.11",
        "Topic :: Software Development :: Libraries",
        "Topic :: Scientific/Engineering :: Artificial Intelligence",
    ],
    keywords=[
        "llm", "observability", "openai",
        "anthropic", "groq", "gateway",
        "hallucination", "monitoring"
    ],
)
