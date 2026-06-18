# Contributing to Ajah

Thanks for your interest in contributing. Ajah is an open-source LLM observability gateway — contributions of all kinds are welcome: bug fixes, new features, documentation, and tests.

---

## Table of Contents

- [Development Setup](#development-setup)
- [Running Tests](#running-tests)
- [Submitting a Pull Request](#submitting-a-pull-request)
- [Code Style Guidelines](#code-style-guidelines)
- [Project Structure](#project-structure)
- [Reporting Bugs](#reporting-bugs)

---

## Development Setup

### Prerequisites

| Tool | Version | Purpose |
|---|---|---|
| Go | 1.22+ | Gateway and pipeline |
| Python | 3.11+ | Quality scorer |
| Node.js | 20+ | Dashboard |
| Docker + Compose | Latest | Full stack |

### 1. Clone and configure

```bash
git clone https://github.com/VigneshReddy-afk/ajah
cd ajah
cp .env.example .env
```

Edit `.env` and set at minimum:

```
CLICKHOUSE_PASSWORD=your_local_password
```

### 2. Start the full stack

```bash
docker-compose up -d
```

All six services start together: Gateway (`:8080`), Scorer (`:8001`), Dashboard (`:3000`), ClickHouse, Redis, PostgreSQL.

### 3. Run services individually for development

**Gateway (Go)**

```bash
go mod download
go run ./cmd/gateway
```

**Scorer (Python)**

```bash
cd scorer
python -m venv .venv
source .venv/bin/activate   # Windows: .venv\Scripts\activate
pip install -r requirements.txt
uvicorn main:app --reload --port 8001
```

**Dashboard (React/TypeScript)**

```bash
cd dashboard
npm install
npm run dev
```

Dashboard dev server runs at `localhost:5173` with hot reload.

---

## Running Tests

### Go (gateway + pipeline)

```bash
# All tests
go test ./...

# With race detector
go test -race ./...

# Specific package
go test ./internal/masking/...
go test ./internal/attribution/...
```

Or via Make:

```bash
make test
make test-integration
```

### Python (scorer)

```bash
cd scorer
pip install -r requirements-dev.txt
pytest
```

### TypeScript (dashboard)

```bash
cd dashboard
npm run build   # type-check via tsc -b
```

There are no browser tests yet — a passing `tsc -b && vite build` is the current bar.

### Smoke test against a running stack

```bash
curl -s -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -H "X-User-ID: test-user" \
  -H "X-Feature-Name: smoke-test" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"ping"}]}'
```

A `200` response with a valid completion means the gateway, async pipeline, and storage are all healthy.

---

## Submitting a Pull Request

1. **Fork** the repository and create a branch from `master`:

   ```bash
   git checkout -b feat/your-feature-name
   ```

2. **Make your changes.** Keep commits focused — one logical change per commit.

3. **Ensure tests pass** before opening a PR:

   ```bash
   go test ./...
   cd dashboard && npm run build
   ```

4. **Write a clear commit message.** We follow [Conventional Commits](https://www.conventionalcommits.org):

   | Prefix | When to use |
   |---|---|
   | `feat:` | New feature |
   | `fix:` | Bug fix |
   | `docs:` | Documentation only |
   | `refactor:` | Code change with no feature or fix |
   | `test:` | Adding or updating tests |
   | `chore:` | Build, CI, dependency updates |

5. **Open a PR** against `master`. In the description include:
   - What the change does and why
   - How to test it manually
   - Screenshots for UI changes

6. **Keep the PR small.** Large refactors should be discussed in an issue first.

---

## Code Style Guidelines

### Go

- Standard `gofmt` formatting.
- Exported functions must have a doc comment.
- Errors are returned, not panicked (except in `main` startup).
- Keep the async pipeline handlers fast — no blocking I/O on the hot path.
- New packages under `internal/` — nothing from `cmd/` should be imported by the pipeline.

### Python (scorer)

- `black` formatting, `isort` imports.
- Type annotations on all public functions.
- ML model loading happens once at startup — not per-request.

### TypeScript (dashboard)

- All styling via inline styles or the CSS classes in `index.css` — no new Tailwind utilities.
- No `any` types except in Recharts shape callbacks (which require it).
- New pages go in `dashboard/src/pages/`, new shared components in `dashboard/src/components/`.
- Keep components co-located with their data-fetching logic using `@tanstack/react-query`.

### General

- No commented-out code in PRs.
- No `console.log` / `fmt.Println` left in production paths.
- Tests required for any new business logic in `internal/`.

---

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
| `helm/` | Helm chart for Kubernetes deployment |

See [README.md](README.md) for the full system diagram and architecture overview.

---

## Reporting Bugs

Open an issue at [github.com/VigneshReddy-afk/ajah/issues](https://github.com/VigneshReddy-afk/ajah/issues) and include:

- **What you did** — the exact request or action
- **What you expected** — the intended behaviour
- **What happened** — actual output, error message, or log line
- **Environment** — OS, Docker version, Go/Python/Node version
- **Reproduction steps** — minimal curl command or code snippet

For security vulnerabilities, please email **vigneshreddy181200@gmail.com** directly rather than opening a public issue.

---

## Questions?

- Open a [GitHub Discussion](https://github.com/VigneshReddy-afk/ajah/issues) for design questions or feature ideas.
- Join the [Discord](https://discord.gg/JktkwHbWx) for real-time chat.
