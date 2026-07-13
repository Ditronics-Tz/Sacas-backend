# SACAS Backend

Go API for the SACAS university timetable system.

**Repo:** https://github.com/Ditronics-Tz/Sacas-backend  
**Frontend (separate):** https://github.com/Ditronics-Tz/timetable_ui

## Stack

- Go 1.23 + Gin + GORM + PostgreSQL + Redis + JWT
- Optional constraint solver: `solver-service/` (Python FastAPI + OR-Tools CP-SAT)

## Quick start

```bash
cp .env.example .env
# start Postgres + Redis + solver + API
docker compose up --build
```

API: `http://localhost:8080`  
Health: `http://localhost:8080/api/health`  
Solver: `http://localhost:8090/health`

Seed admin: `admin@example.com` / `password`

### Manual run

```bash
# Postgres + Redis running locally
go run ./cmd/api

# optional solver
cd solver-service && pip install -r requirements.txt
set PYTHONPATH=.
uvicorn app.main:app --port 8090
```

## Docs (this repo only)

| File | Topic |
|------|--------|
| [docs/backend.md](./docs/backend.md) | Architecture, models, auth |
| [docs/api-contract.md](./docs/api-contract.md) | REST endpoints & payloads |
| [docs/solver-contract.md](./docs/solver-contract.md) | Solver request/response |
| [docs/setup.md](./docs/setup.md) | Local setup, CSRF, CORS |
| [docs/domain-mapping.md](./docs/domain-mapping.md) | UI labels ↔ models |
| [docs/integration.md](./docs/integration.md) | FE/BE alignment notes |
| [DECISIONS.md](./DECISIONS.md) | Product/engineering decisions |

## Layout

```
cmd/api/           # entrypoint
internal/          # config, controllers, models, routes, services
solver-service/    # OR-Tools CP-SAT microservice
docs/              # backend documentation
docker-compose.yml # postgres, redis, solver, api
```

## Tests

```bash
go test ./...
go vet ./...
cd solver-service && pytest
```
