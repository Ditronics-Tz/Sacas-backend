# Local setup — backend (Windows Go commands preferred)

## Prerequisites

| Tool | Version / notes |
|------|------------------|
| Go | 1.23+ — `winget install GoLang.Go` |
| PostgreSQL | local install — `winget install PostgreSQL.PostgreSQL` |
| Redis | Memurai / WSL redis / any server on `localhost:6379` |
| Python | optional — only if you run `solver-service` |
| Docker | **optional** — not required |

Default admin after first boot: `admin@example.com` / `password` (`super_admin`).

---

## Windows: normal Go commands (no Docker)

See also root **[README.md](../README.md)** section *Windows — run with normal Go commands*.

### 1. One-time installs

```powershell
winget install GoLang.Go
winget install PostgreSQL.PostgreSQL
# Redis: install Memurai (Windows) or run Redis in WSL
```

### 2. Create database

```sql
CREATE DATABASE "SACAS";
```

### 3. Start API

From **this repo root** (`Sacas-backend/`):

```powershell
copy .env.example .env
# Edit DATABASE_URL password if needed
# Leave SOLVER_URL empty for greedy scheduler (easiest testing)

.\run.ps1
# equivalent:
#   go mod tidy
#   go run ./cmd/api
```

### 4. Smoke test

```powershell
Invoke-RestMethod http://localhost:8080/api/health

Invoke-RestMethod -Method POST -Uri http://localhost:8080/api/auth/login `
  -ContentType application/json `
  -Body '{"email":"admin@example.com","password":"password"}'
```

### Docker (optional only)

```powershell
docker compose up --build
```

---

## Manual local notes

### 1. PostgreSQL + Redis

```sql
CREATE DATABASE "SACAS";
```

Redis must accept connections at `REDIS_ADDR` (default `localhost:6379`).

### 2. Backend

```powershell
cd Sacas-backend
copy .env.example .env
go mod tidy
go run ./cmd/api
```

CORS preflight check (PowerShell):

```powershell
curl.exe -i -X OPTIONS http://localhost:8080/api/health `
  -H "Origin: http://localhost:5173" `
  -H "Access-Control-Request-Method: GET"
```

Expect `204` with `Access-Control-Allow-Origin` and related headers.

### CSRF (when `CSRF_ENABLED=true`)

SPA flow:

1. On load, frontend calls `GET /api/csrf` (issues Redis-backed token + `X-CSRF-Token` header + `csrf_token` cookie).
2. Every mutating request sends **`X-CSRF-Token` header** (cookie alone is rejected).
3. If a `csrf_token` cookie is present, the header must match it (double-submit).

```powershell
# Bootstrap
$r = Invoke-WebRequest http://localhost:8080/api/csrf -SessionVariable s
$token = $r.Headers['X-CSRF-Token']
# Mutate with header
Invoke-RestMethod -Method POST -Uri http://localhost:8080/api/auth/login `
  -WebSession $s -ContentType application/json `
  -Headers @{ 'X-CSRF-Token' = $token } `
  -Body '{"email":"admin@example.com","password":"password"}'
```

Local SPA dev: keep `CSRF_ENABLED=false` unless testing the full flow.

Health:

```powershell
Invoke-RestMethod http://localhost:8080/api/health
```

### 3. Solver (optional)

Skip this for normal testing. Empty `SOLVER_URL` uses the Go greedy generator.

```powershell
cd solver-service
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
$env:PYTHONPATH = "."
uvicorn app.main:app --host 0.0.0.0 --port 8090
```

Then set `SOLVER_URL=http://localhost:8090` in `.env` and restart the API.

### 4. Frontend (separate repo: timetable_ui)

```powershell
cd ..\timetable_ui
copy .env.example .env
# VITE_API_URL=http://localhost:8080/api
npm install
npm run dev
```

Open http://localhost:5173.

---

## Suggested two-terminal workflow (Windows, no Docker)

| Terminal | Command |
|----------|---------|
| 1 — API | `cd Sacas-backend` → `.\run.ps1` (or `go run ./cmd/api`) |
| 2 — UI | `cd timetable_ui` → `npm run dev` |

Solver terminal only if `SOLVER_URL` is set.

---

## Common failures

| Symptom | Likely cause |
|---------|----------------|
| CORS error | Origin not in `CORS_ALLOWED_ORIGINS` |
| 403 CSRF | `CSRF_ENABLED=true` without token — set false for SPA dev |
| Login 403 inactive | Verify email first |
| Timetable 403 | Need admin role |
| Solver timeout / greedy | `SOLVER_URL` down; check `engine` in generate response |
| Docker backend no .env | Env vars come from compose — OK |

---

## Tests

```bash
# Backend
cd Sacas-backend && go test ./... && go vet ./...

# Frontend
cd timetable_ui && npm test

# Solver
cd solver-service && pytest -q
```
