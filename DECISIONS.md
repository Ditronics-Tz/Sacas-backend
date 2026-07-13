# Design Decisions

Judgment calls made while implementing SACAS across all phases.

## Phase 1

### CORS
- Used hand-rolled Gin CORS middleware (`internal/middlewares/cors.go`) rather than `gin-contrib/cors` to avoid an extra dependency and keep allowlist logic explicit.
- `CORS_ALLOWED_ORIGINS` is comma-separated; defaults to `http://localhost:3000,http://localhost:5173` (CRA + Vite).

### CSRF
- Default for SPA development: `CSRF_ENABLED=false` in `.env.example`.
- Startup always logs whether CSRF is ON or OFF.
- When `CSRF_ENABLED=true`: SPA bootstraps via `GET /api/csrf`; mutations must send `X-CSRF-Token` header (cookie alone rejected). Header must match cookie when cookie present (double-submit). Redis required (fail closed).
- Rate limit on auth/OTP fails closed if Redis is down.

### Legacy seed (`database/seed.go`)
- **Decision:** Align legacy `SeedDB` with `CreateInitialData` — use role `super_admin` and the same bcrypt hash for password `password`. Do not delete the file (may be referenced later) but main path uses `CreateInitialData` only.
- Role string `"admin"` is invalid; valid roles are `user`, `administrator`, `super_admin`.

### Health check
- `GET /api/health` reports `status`, `db`, `redis`, `timestamp`, `version`. Overall `status` is `degraded` if either dependency is down (process still responds).

## Phase 4 / 7 — Model extensions

Extended models rather than dropping UI fields:

| Entity | New fields |
|--------|------------|
| Faculty | `hod_name`, `hod_phone`, `hod_email` (nullable) |
| Course | `level` (nullable string, e.g. NTA/diploma/degree) |
| Module | `code`, `semester` (int), `nta_level` (nullable) |
| Class | `academic_year` (nullable string, e.g. "2024/25") |
| Staff | `rfid_id`, `phone_number`, `title`, `staff_type` (nullable) |

Room building/room_no/description live in `features` JSON (extended shape documented in domain-mapping).

### Staff preferences JSON shape
```json
{
  "unavailable_days": ["saturday", "sunday"],
  "preferred_start": "08:00",
  "day_offs": ["friday"],
  "unavailable_slots": [],
  "preferred_times": ["morning"],
  "max_consecutive": 4,
  "travel_buffer": 0
}
```
`unavailable_days` is the canonical field used by the solver; `day_offs` is accepted as an alias for backward compatibility.

## Phase 7 — Allocations & Subjects
- Staff↔Module allocation uses GORM many2many `staff_modules` with explicit REST endpoints.
- Subject CRUD exposed under `/protected/timetable/subjects`.

## Phase 8 — Frontend toolchain
- Migrated CRA → **Vite** + **Vitest** (native to Vite, least churn for green suite).
- Env key: `VITE_API_URL` (replaces `REACT_APP_API_URL`).

## Phase 9 — Solver
- **Option A chosen:** Python FastAPI microservice with Google OR-Tools CP-SAT (`solver-service/`).
- Go backend calls via `internal/services/solverclient.go`; greedy generator retained only as fallback when `SOLVER_URL` is empty or solver unreachable (`SOLVER_FALLBACK=true` default).
- Contract: `docs/solver-contract.md`.

## Phase 10
- Root `docker-compose.yml` orchestrates Postgres, Redis, backend, solver, frontend.
- E2E: Playwright smoke script under `e2e/` where tooling allows; manual smoke checklist documented if browsers not installed.
