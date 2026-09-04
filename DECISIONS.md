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

## RBAC audit (2026-07-13)

Timetable group already used AdminMiddleware. Hardened RoleMiddleware type safety, added RequireRole factory + regression tests. Live probe: role=user → 403 on GET /timetable/faculties. Full matrix: docs/RBAC_AUDIT.md.

## User ↔ Staff linkage (2026-09-04)

**Decision: nullable `Staff.UserID` FK (unique) — NOT `User.StaffID`.**

- `Staff` already has `Email` but no FK to `User`; `User` is the auth/login account.
- Options considered: `User.StaffID` vs `Staff.UserID`. Chose `Staff.UserID *uint` (`gorm:"uniqueIndex"`, `foreignKey:UserID`, nullable) because:
  1. `Staff` is the dependent entity (a login account may exist without a staff profile; a staff row may exist without a login account — e.g. imported staff). Making `User` hold `StaffID` would force every user to have a staff slot and imply a user can have at most one staff, but encoded on the wrong side.
  2. `Staff.UserID` yields a clean 1:1 nullable unique index: at most one `Staff` per `User`, multiple staff can be unlinked (`NULL` not conflicting).
  3. Queries needed are `WHERE staffs.user_id = ?` (resolve authenticated `user_id` → staff). Index on `staffs.user_id` is efficient; reverse would require `WHERE users.staff_id = ?` plus join.
  4. Migration is a single additive nullable column via `AutoMigrate`; no backfill of `users` table, no risk to existing auth flows.

**Migration:** `internal/database/migrations.go:RunMigrations` `AutoMigrate(&Staff{})` adds `user_id` column + unique index (`staffs.user_id`). Idempotent `BackfillStaffUserLinks()` runs once on boot: `UPDATE staffs SET user_id = users.id WHERE user_id IS NULL AND email IN (SELECT email FROM users)` — server-side only, never overwrites an existing link, logged row count. This is *not* per-request silent matching; it is a one-time bootstrap for legacy deployments that had matching emails before the FK existed.

**Linking policy (admin creation):**
- `POST /timetable/staff` and `PUT /timetable/staff/:id` accept optional `user_id` (admin-only). The controller validates `user_id` exists and is not already linked to another staff; duplicate link → `400` `"user_id X is already linked to staff Y"`. Missing user → `400`.
- **Never auto-link by email on create/update** — the handler does not inspect `staff.email` to find a user. An admin must explicitly supply `user_id` (selected from a user list or confirmed by matching email in UI with explicit confirmation). This prevents silent misattribution on typos/collisions.

**Endpoints that rely on this link (trusted path):**
- `GET /api/protected/me/staff` → returns `{"staff": <Staff>}` linked to `JWT user_id`; `404 {"error": "No staff profile is linked to your account", "staff": null}` if none — not `500`.
- `GET /api/protected/timetable/my` → resolves `user_id → staff → timetables`; same 404 semantics.
- Both ignore any client-supplied `staff_id` query/path/body values.

**Security invariant:** verified by `internal/controllers/timetable_my_test.go` and `internal/controllers/me_staff_test.go`.

