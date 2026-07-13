# SACAS Backend

Go API for the **SACAS** university timetable system (auth, academic entities, allocations, timetable generate/preview).

| | |
|--|--|
| **Repo** | https://github.com/Ditronics-Tz/Sacas-backend |
| **Frontend (separate)** | https://github.com/Ditronics-Tz/timetable_ui |
| **Default API** | http://localhost:8080 |
| **Health** | http://localhost:8080/api/health |

## Stack

- **Go** 1.23+ · **Gin** · **GORM** · **PostgreSQL** · **JWT**
- **Redis** optional in development (OTP / rate-limit / CSRF store when enabled)
- **Solver** optional: `solver-service/` (Python FastAPI + OR-Tools CP-SAT) — leave `SOLVER_URL` empty to use the built-in greedy engine

---

## Quick start (Windows, no Docker)

### 1. Prerequisites

```powershell
winget install GoLang.Go
winget install PostgreSQL.PostgreSQL
go version
```

```sql
CREATE DATABASE "SACAS";
```

**Redis is optional for local dev.** If offline, the API still boots; login + CRUD work. OTP / rate-limit need Redis when those features are used.

### 2. Configure `.env`

```powershell
cd Sacas-backend
copy .env.example .env
# DATABASE_URL=host=localhost user=postgres password=YOUR_PASSWORD dbname=SACAS port=5432 sslmode=disable
```

Recommended:

```env
ENV=development
CSRF_ENABLED=false
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:5173
SOLVER_URL=
LOG_OPTIONS=false
```

### 3. Run the dev server

```powershell
go run .
```

Also: `go run ./cmd/api` · `.\run.ps1`  
Stop: **Ctrl+C**

Logs show **requests + errors only** (no Gin route spam). Set `LOG_OPTIONS=true` to log CORS OPTIONS.

Migrations + demo users seed automatically on boot.

### 4. Smoke test

```powershell
Invoke-RestMethod http://localhost:8080/api/health

Invoke-RestMethod -Method POST -Uri http://localhost:8080/api/auth/login `
  -ContentType application/json `
  -Body '{"email":"admin@example.com","password":"password"}'
```

---

## Demo accounts

All passwords: **`password`** — see **[DEMO_ACCOUNTS.txt](./DEMO_ACCOUNTS.txt)**

| Email | Role |
|-------|------|
| `admin@example.com` | `super_admin` |
| `coordinator@sacas.local` | `administrator` |
| `scheduler@sacas.local` | `administrator` |
| `lecturer@sacas.local` | `user` (no timetable admin API) |
| `viewer@sacas.local` | `user` |

```powershell
go run ./cmd/seed_demo   # re-seed / refresh demos
```

---

## RBAC (security)

| Role | Access |
|------|--------|
| `user` | Profile / change-password only under `/api/protected` |
| `administrator` | Users admin + all `/api/protected/timetable/*` + `/admin/*` |
| `super_admin` | Everything + `/api/protected/superadmin/*` |

- Role comes **only** from verified JWT claims (never from client headers/body).
- All timetable routes use `AdminMiddleware` on the group.
- Audit + matrix: **[docs/RBAC_AUDIT.md](./docs/RBAC_AUDIT.md)**
- Prefer wiring new routes with `middlewares.RequireRole("administrator", "super_admin")`.

```powershell
# role=user must get 403
# Authorization: Bearer <lecturer_token>
# GET /api/protected/timetable/faculties  → 403
```

---

## Useful commands

```powershell
go mod tidy
go run .                    # dev server
go test ./...
go vet ./...
go build -o bin\api.exe .
.\test.ps1
```

**Port in use:**

```powershell
netstat -ano | findstr :8080
Stop-Process -Id <PID> -Force
# or PORT=8081 in .env
```

### Optional solver

```powershell
cd solver-service
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
$env:PYTHONPATH = "."
uvicorn app.main:app --host 0.0.0.0 --port 8090
```

```env
SOLVER_URL=http://localhost:8090
```

### Docker (optional)

```powershell
docker compose up --build
```

---

## Layout

```
main.go                 # go run .
cmd/api/                # alternate entry
cmd/seed_demo/          # re-seed demo users
internal/
  app/ middlewares/ routes/ controllers/ …
  services/             # timetable + solver client
solver-service/         # optional OR-Tools
docs/                   # API, RBAC audit, setup
HOW_TO_USE.md
DEMO_ACCOUNTS.txt
.env.example
```

## Docs index

| File | Topic |
|------|--------|
| [HOW_TO_USE.md](./HOW_TO_USE.md) | Hands-on API usage |
| [DEMO_ACCOUNTS.txt](./DEMO_ACCOUNTS.txt) | Demo logins |
| [docs/RBAC_AUDIT.md](./docs/RBAC_AUDIT.md) | Role × endpoint matrix |
| [docs/api-contract.md](./docs/api-contract.md) | REST contract |
| [docs/backend.md](./docs/backend.md) | Architecture |
| [docs/setup.md](./docs/setup.md) | Setup detail |
| [docs/solver-contract.md](./docs/solver-contract.md) | Solver JSON |
| [DECISIONS.md](./DECISIONS.md) | Engineering decisions |
