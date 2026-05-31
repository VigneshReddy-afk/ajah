# Contributing to Ajah

## Prerequisites

- Go 1.22+
- Docker Desktop
- Python 3.11+ (runs inside Docker, not needed locally)
- Node.js 18+ (for dashboard development only)

## Quick Start for Contributors

```bash
git clone https://github.com/VigneshReddy-afk/ajah
cd ajah
cp .env.example .env
docker-compose up -d
```

## Running Tests

```bash
make test
make test-integration
```

## Project Structure

| Path | Description |
|---|---|
| `cmd/gateway/` | Go gateway entry point |
| `internal/` | Go packages (proxy, attribution, masking, sessions, flagging, storage) |
| `scorer/` | Python FastAPI quality scorer |
| `dashboard/` | React TypeScript frontend |
| `tests/` | End-to-end test scripts |
| `landing/` | useajah.com static site |
| `examples/` | LangChain and CrewAI examples |

## Submitting a PR

- One feature per PR
- Tests required for all Go changes
- Run `make test` before submitting
- Clear commit message describing what and why

## Architecture

See [README.md](README.md) for the full system diagram.
