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

Create the database (pgAdmin or `psql`):

```sql
CREATE DATABASE "SACAS";
```

**Redis is optional for local dev.** If Redis is offline, the API still boots; login + CRUD work. OTP email verify / rate-limit need Redis.

### 2. Configure `.env`

```powershell
cd Sacas-backend
copy .env.example .env
# Edit DATABASE_URL — example:
# DATABASE_URL=host=localhost user=postgres password=YOUR_PASSWORD dbname=SACAS port=5432 sslmode=disable
```

Recommended local flags:

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

Also valid:

```powershell
go run ./cmd/api
.\run.ps1
```

Stop: **Ctrl+C**

On success you get a short banner, then **request + error logs only** (no Gin route spam). CORS `OPTIONS` preflight is hidden unless `LOG_OPTIONS=true`.

### 4. Smoke test

```powershell
Invoke-RestMethod http://localhost:8080/api/health

Invoke-RestMethod -Method POST -Uri http://localhost:8080/api/auth/login `
  -ContentType application/json `
  -Body '{"email":"admin@example.com","password":"password"}'
```

Migrations + demo seed run automatically on every boot.

---

## Demo accounts

All passwords: **`password`**

| Email | Role | Use for |
|-------|------|---------|
| `admin@example.com` | `super_admin` | Full access |
| `coordinator@sacas.local` | `administrator` | Timetable admin UI |
| `scheduler@sacas.local` | `administrator` | Timetable admin UI |
| `lecturer@sacas.local` | `user` | Permissions test (no admin screens) |
| `viewer@sacas.local` | `user` | Permissions test |

Full list: **[DEMO_ACCOUNTS.txt](./DEMO_ACCOUNTS.txt)**  
Re-seed without restarting (or on next `go run .`):

```powershell
go run ./cmd/seed_demo
```

---

## Useful commands

```powershell
go mod tidy                 # deps
go run .                    # dev server
go test ./...               # unit tests
go vet ./...
go build -o bin\api.exe .   # binary
.\test.ps1                  # test + vet + build
```

### Port already in use (`bind :8080`)

```powershell
netstat -ano | findstr :8080
Stop-Process -Id <PID> -Force
# or set PORT=8081 in .env
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

Brings up Postgres, Redis, solver, and API.

---

## Layout

```
main.go                 # go run . entry
cmd/api/                # alternate entry (same app)
cmd/seed_demo/          # re-seed demo users
internal/
  app/                  # bootstrap
  controllers/
  database/             # migrate + seed
  middlewares/          # CORS, CSRF, JWT, rate limit, request logger
  models/
  repositories/
  routes/
  services/             # timetable + solver client
solver-service/         # optional OR-Tools microservice
docs/                   # API + setup docs
HOW_TO_USE.md           # hands-on API guide
DEMO_ACCOUNTS.txt       # demo logins
.env.example
```

## Docs

| File | Topic |
|------|--------|
| [HOW_TO_USE.md](./HOW_TO_USE.md) | Dev server, sample API calls |
| [DEMO_ACCOUNTS.txt](./DEMO_ACCOUNTS.txt) | Demo emails / passwords |
| [docs/api-contract.md](./docs/api-contract.md) | REST contract |
| [docs/backend.md](./docs/backend.md) | Architecture |
| [docs/setup.md](./docs/setup.md) | Full setup |
| [docs/solver-contract.md](./docs/solver-contract.md) | Solver JSON |
| [DECISIONS.md](./DECISIONS.md) | Engineering decisions |

## Auth notes

- Timetable routes require JWT + role **`administrator`** or **`super_admin`**
- Header: `Authorization: Bearer <token>`
- With `CSRF_ENABLED=false` (default for SPA local), no CSRF header needed
