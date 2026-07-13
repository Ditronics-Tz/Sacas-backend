# Backend — Sacas-backend

**Module name:** `go_boilerplate`  
**Entry point:** `cmd/api/main.go`  
**Default port:** `8080`  
**API prefix:** `/api` (not `/api/v1`)

---

## Stack

- **Language:** Go 1.23
- **HTTP:** Gin
- **ORM:** GORM + PostgreSQL
- **Cache / OTP / CSRF:** Redis
- **Auth:** JWT (HMAC), bcrypt passwords, optional HTTP-only cookie `token`
- **Email OTP:** SendGrid
- **SMS OTP:** Twilio

---

## Directory structure

```
Sacas-backend/
├── cmd/api/main.go              # Boot: env, DB, Redis, routes, server
├── internal/
│   ├── config/                  # GetEnv helpers
│   ├── controllers/             # HTTP handlers
│   ├── database/                # InitDB, migrations, seed, Redis
│   ├── middlewares/             # JWT, RBAC, CSRF, security, metrics
│   ├── models/                  # GORM entities
│   ├── repositories/            # Data access
│   ├── routes/routes.go         # Route table
│   └── services/                # Notification + timetable generation
└── pkg/logger/                  # Logging
```

### Layering

```
HTTP Request
  → middlewares (security, CSRF, JWT, Admin)
  → controllers (bind JSON, status codes)
  → services (business rules — timetable conflicts/generate)
  → repositories (GORM)
  → models / PostgreSQL
```

---

## Boot sequence

From `cmd/api/main.go`:

1. Load `.env` via `godotenv` (**fails hard** if missing).
2. Init logger.
3. Connect PostgreSQL → `RunMigrations` → `CreateInitialData` (super admin).
4. Connect Redis.
5. Wire OTP + notification services.
6. `routes.SetupRoutes(...)`.
7. Listen on `PORT` (default `8080`).

---

## Models (data model)

| Model | File | Key fields | Notes |
|-------|------|------------|--------|
| **User** | `user.go` | email, password, first_name, last_name, phone_number, role, is_active, is_verified | Auth accounts (not teaching staff) |
| **Faculty** | `faculty.go` | name, description | UI “Department” |
| **Course** | `course.go` | name, faculty_id, description | UI “Program” |
| **Module** | `module.go` | name, course_id?, credit_hours, type, requires_lab | Types: `core`, `elective`, `general_subject` |
| **Class** | `class.go` | name, course_id, year (1–6), number_of_students | Student cohort |
| **Room** | `room.go` | name, capacity, features (JSON), sticky, allowed_courses (JSON) | Lab/projector etc. in JSON |
| **Staff** | `staff.go` | name, email, faculty_id, preferences (JSON), max_hours | M2M with modules via `staff_modules` |
| **Subject** | `subject.go` | name, credit_hours | General subjects (not course modules) |
| **Timetable** | `timetable.go` | class_id, module_id?, subject_id?, staff_id, room_id, day, start_time, end_time | Either module **or** subject |

### Roles (`User.role`)

| Value | Access |
|-------|--------|
| `user` | Own profile only |
| `administrator` | Users + all `/api/protected/timetable/*` |
| `super_admin` | Everything + superadmin dashboard |

**Important:** All timetable management routes require **admin or super_admin** via `AdminMiddleware`.

### Weekdays (timetable)

`monday` | `tuesday` | `wednesday` | `thursday` | `friday` | `saturday` | `sunday`  
Times are strings `"HH:MM"` (e.g. `"08:00"`, `"09:00"`).

---

## Auth flow

### Register

- `POST /api/auth/register`
- Creates user with `role=user`, `is_active=false`, `is_verified=false`
- Stores OTP in Redis key `verify:{email}` (15 min)
- Sends email OTP (if SendGrid configured)

### Verify email

- `POST /api/auth/verify-email` with `{ email, otp }`
- Sets `is_verified=true` and `is_active=true`

### Login

- `POST /api/auth/login` with `{ email, password }`
- Rejects if inactive
- Returns JWT (24h) + user object; sets cookie `token`

### JWT claims

```json
{
  "user_id": 1,
  "role": "super_admin",
  "email": "admin@example.com",
  "exp": 1234567890,
  "iat": 1234567890
}
```

Accepted on protected routes via:

- Header: `Authorization: Bearer <token>`, **or**
- Cookie: `token`

---

## Security middlewares

| Feature | Default | Impact on frontend |
|---------|---------|---------------------|
| JWT on `/api/protected/*` | Always | Must send Bearer token |
| Admin on timetable routes | Always | Login as admin/super_admin |
| CSRF (`CSRF_ENABLED`) | `true` | POSTs need `X-CSRF-Token` (Redis-backed) |
| Security headers / input scans | On | Large payloads limited (10MB) |
| CORS | **Not implemented** | Browser calls from `:3000` will fail until added |

For local SPA development, typical choices:

- Set `CSRF_ENABLED=false`, **and**
- Add CORS allowing `http://localhost:3000` with `Authorization` header.

---

## Timetable service

`internal/services/timetable.go`:

- **GenerateTimetable(classID):** loads class → course modules + all general subjects → schedules sessions Mon–Fri on fixed slots (`08:00`–`16:00`, skip noon).
- Picks staff (module’s assigned staff or any free staff for subjects) and rooms (capacity + lab if needed).
- **ValidateTimeSlot:** conflict checks via repository.

Endpoints:

- `POST /api/protected/timetable/generate` body `{ "class_id": 1 }`
- Manual CRUD under `/api/protected/timetable/`

---

## Missing / incomplete backend pieces (for planning)

1. **No CORS middleware** — required for React dev server.
2. **Subject CRUD** — model + repo exist; **no controller/routes** exposed.
3. **Staff ↔ Module allocation API** — GORM M2M `staff_modules` exists; **no explicit assign/unassign endpoints**.
4. **No PUT** for course, module, class, room (create/get/delete only).
5. **Legacy seed** `database/seed.go` uses role `"admin"` (wrong); real seed is `CreateInitialData` with `super_admin`.
6. **Notification** fails silently if SendGrid/Twilio env empty (registration still creates user).

---

## Run / test commands

```bash
cd Sacas-backend
cp .env.example .env   # edit DATABASE_URL, REDIS, JWT_SECRET
go mod tidy
go run ./cmd/api

# tests / lint (if tooling installed)
go test ./...
golangci-lint run
go build ./cmd/...
```

See [setup.md](./setup.md) for full prerequisites.
