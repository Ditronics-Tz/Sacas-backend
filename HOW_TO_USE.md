# How to use the SACAS backend (dev)

## Start the server

Open PowerShell:

```powershell
cd C:\Users\User\Projects\timetable\Sacas-backend

# first time only: create .env
if (-not (Test-Path .env)) { copy .env.example .env }

# START DEV SERVER
go run .
```

Leave that window open. You should see:

```
SACAS API is running
Base URL : http://localhost:8080
Health   : http://localhost:8080/api/health
```

Stop: **Ctrl+C**

Same server:

```powershell
go run ./cmd/api
.\run.ps1
```

---

## Before first run (once)

1. **Go installed** — `go version` (you already have this)
2. **PostgreSQL running** + database:

```sql
CREATE DATABASE "SACAS";
```

3. **Redis is optional in development** — if Redis is not running, the API still starts.
   - Login as admin + CRUD timetable APIs work
   - Email OTP / password-reset OTP need Redis later (Memurai on Windows is fine)
4. Edit `.env` if your Postgres password is not `postgres`:

```env
DATABASE_URL=host=localhost user=postgres password=YOUR_PASSWORD dbname=SACAS port=5432 sslmode=disable
ENV=development
```

---

## How to call the API (learn by doing)

Open a **second** PowerShell while the server is running.

### 1) Health

```powershell
Invoke-RestMethod http://localhost:8080/api/health
```

Expect: `status` ok, `db` up, `redis` up.

### 2) Login (demo accounts)

All demo passwords: **`password`**

| Email | Role | What they can do |
|-------|------|------------------|
| `admin@example.com` | `super_admin` | Full access (users + all timetable admin) |
| `coordinator@sacas.local` | `administrator` | Timetable admin (faculties, rooms, generate, …) |
| `scheduler@sacas.local` | `administrator` | Same as coordinator |
| `lecturer@sacas.local` | `user` | Profile only — **no** timetable admin screens |
| `viewer@sacas.local` | `user` | Profile only — test “insufficient permissions” |

```powershell
$login = Invoke-RestMethod -Method POST -Uri http://localhost:8080/api/auth/login `
  -ContentType application/json `
  -Body '{"email":"admin@example.com","password":"password"}'

$login
$token = $login.token
```

Re-seed demos anytime (server can stay running):

```powershell
go run ./cmd/seed_demo
```

### 3) Call a protected route

```powershell
$headers = @{ Authorization = "Bearer $token" }

# Admin dashboard stats
Invoke-RestMethod http://localhost:8080/api/protected/admin/dashboard -Headers $headers

# List faculties (departments)
Invoke-RestMethod http://localhost:8080/api/protected/timetable/faculties -Headers $headers
```

### 4) Create a faculty (department)

```powershell
Invoke-RestMethod -Method POST `
  -Uri http://localhost:8080/api/protected/timetable/faculties `
  -Headers $headers `
  -ContentType application/json `
  -Body '{"name":"Computer Science","description":"CS faculty"}'
```

### 5) Browser UI (optional)

In another terminal:

```powershell
cd C:\Users\User\Projects\timetable\timetable_ui
# ensure .env has: VITE_API_URL=http://localhost:8080/api
npm run dev
```

Open http://localhost:5173 → login with the same admin.

---

## Common errors

| Message | Fix |
|---------|-----|
| `Failed to connect to Redis` | Start Redis/Memurai on port 6379 |
| Postgres connection error | Start Postgres; check `DATABASE_URL` password; create DB `SACAS` |
| `go: command not found` | Install Go, reopen terminal |
| Port in use | Change `PORT=8081` in `.env` or stop other process on 8080 |

---

## Full endpoint list

See [docs/api-contract.md](./docs/api-contract.md).
