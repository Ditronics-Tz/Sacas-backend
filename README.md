# SACAS Backend

Go API for the SACAS university timetable system.

**Repo:** https://github.com/Ditronics-Tz/Sacas-backend  
**Frontend (separate):** https://github.com/Ditronics-Tz/timetable_ui

## Stack

- Go 1.23 + Gin + GORM + PostgreSQL + Redis + JWT
- Optional constraint solver: `solver-service/` (Python + OR-Tools) — **not required** for basic testing

---

## Windows — run with normal Go commands (no Docker)

This is the recommended path if you are on Windows without Docker.

### 1. Install tools (once)

Open **PowerShell** (or Terminal):

```powershell
winget install GoLang.Go
winget install PostgreSQL.PostgreSQL
```

Close and reopen the terminal so `go` and `psql` are on PATH.

Check:

```powershell
go version
```

### 2. PostgreSQL database (once)

Using **pgAdmin** or `psql`:

```sql
CREATE DATABASE "SACAS";
```

Or in PowerShell (adjust password if needed):

```powershell
# If psql is on PATH:
$env:PGPASSWORD = "postgres"
psql -U postgres -h localhost -c 'CREATE DATABASE "SACAS";'
```

### 3. Redis (required for boot)

The API pings Redis at startup. Pick **one**:

| Option | How |
|--------|-----|
| **Memurai** (Redis-compatible, Windows-native) | https://www.memurai.com/get-memurai — install, leave default `localhost:6379` |
| **WSL** | `wsl` then `sudo apt install redis-server && sudo service redis-server start` (use `localhost:6379` from Windows) |
| **Chocolatey** | `choco install redis-64` then start the Redis service |

Quick check:

```powershell
Test-NetConnection localhost -Port 6379
```

### 4. Configure and run the API (dev server)

```powershell
cd path\to\Sacas-backend

# Create .env once (edit password if needed)
copy .env.example .env

# >>> THIS IS THE DEV SERVER <<<
go run .
```

Also works:

```powershell
go run ./cmd/api
.\run.ps1
```

Leave the terminal open. Stop with **Ctrl+C**.  
Usage guide (login, sample calls): **[HOW_TO_USE.md](./HOW_TO_USE.md)**

### 5. Verify

```powershell
# Health (DB + Redis)
Invoke-RestMethod http://localhost:8080/api/health

# Login as seed super admin
Invoke-RestMethod -Method POST -Uri http://localhost:8080/api/auth/login `
  -ContentType application/json `
  -Body '{"email":"admin@example.com","password":"password"}'
```

| Item | Value |
|------|--------|
| API | http://localhost:8080 |
| Health | http://localhost:8080/api/health |
| Seed admin | `admin@example.com` / `password` |

### Useful Go commands

```powershell
go mod tidy                 # download deps
go run .                    # start DEV server  <<< use this
go run ./cmd/api            # same server (alt path)
go test ./...               # unit tests
go vet ./...                # static checks
go build -o bin\api.exe .   # produce binary
.\bin\api.exe               # run binary (needs .env in cwd)
.\test.ps1                  # test + vet + build
```

### Solver (optional — skip for easier testing)

By default `.env.example` sets **`SOLVER_URL=` empty** so the API uses the **built-in greedy** timetable generator. No Python needed.

Only if you want OR-Tools:

```powershell
cd solver-service
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
$env:PYTHONPATH = "."
uvicorn app.main:app --host 0.0.0.0 --port 8090
```

Then in `.env`:

```env
SOLVER_URL=http://localhost:8090
```

---

## Docker (optional)

If you install Docker later:

```powershell
docker compose up --build
```

---

## Docs (this repo)

| File | Topic |
|------|--------|
| [docs/setup.md](./docs/setup.md) | Full setup notes |
| [docs/backend.md](./docs/backend.md) | Architecture |
| [docs/api-contract.md](./docs/api-contract.md) | REST API |
| [docs/solver-contract.md](./docs/solver-contract.md) | Solver JSON |
| [DECISIONS.md](./DECISIONS.md) | Design choices |

## Layout

```
cmd/api/           # go run ./cmd/api
internal/          # app code
solver-service/    # optional Python solver
run.ps1 / run.bat  # Windows start helpers
test.ps1           # go test + vet + build
.env.example       # copy → .env
```
