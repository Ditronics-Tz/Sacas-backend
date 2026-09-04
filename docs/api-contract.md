# API Contract (as implemented in backend)

Base URL (local): `http://localhost:8080`  
Prefix: **`/api`**  
Content-Type: `application/json`

Protected routes need:

```http
Authorization: Bearer <jwt>
```

If `CSRF_ENABLED=true`, mutating requests also need:

```http
X-CSRF-Token: <token from GET response header/cookie>
```

Timetable domain routes also need role **`administrator`** or **`super_admin`**.

---

## System

### GET `/api/health`

```json
{
  "status": "ok",
  "db": "up",
  "redis": "up",
  "timestamp": "2026-01-01T00:00:00Z",
  "version": "1.0.0"
}
```

`status` is `degraded` and HTTP 503 if DB or Redis is down.

### GET `/api/metrics`

Request metrics object (or empty message).

---

## Auth (public)

### POST `/api/auth/register`

**Request**

```json
{
  "email": "user@example.com",
  "password": "secret1",
  "first_name": "Jane",
  "last_name": "Doe",
  "phone_number": "+255700000000"
}
```

**Response** `201`

```json
{
  "message": "User registered successfully. Please check your email for verification code.",
  "user_id": 2
}
```

### POST `/api/auth/login`

```json
{ "email": "admin@example.com", "password": "password" }
```

**Response** `200` — `{ "message", "token", "user": { id, email, first_name, last_name, role, is_active } }`

**Token delivery is dual-channel by design:**
- Browser/SPA clients authenticate via the httpOnly `token` cookie.
- Native/mobile clients use the bearer token from the `token` body field
  (they cannot rely on browser cookie jars).
- Purely SPA deployments can omit the body token by setting
  `AUTH_RETURN_TOKEN_IN_BODY=false` (default `true`) to reduce token exposure.

### Password policy

Applies to `POST /api/auth/register` (`password`) and
`POST /api/auth/reset-password` (`new_password`):

- Minimum 8 characters
- At least one uppercase letter, one lowercase letter, and one digit
- Violations return `400` with:
  `"Password must be at least 8 characters and contain at least one uppercase letter, one lowercase letter, and one digit"`

### OTP verification & attempt lockout

Applies to `POST /api/auth/verify-email`, `POST /api/auth/reset-password`, and
`POST /api/otp/verify`:

- Failed attempts are counted **per account** (per purpose + email) in Redis —
  the per-IP rate limit alone does not stop distributed brute-force attempts.
- After `OTP_MAX_ATTEMPTS` failed attempts (default `5`), the OTP is deleted
  and further attempts return **`429`** with
  `"Too many failed attempts..."` until a fresh code is requested.
- The attempt counter expires with the OTP's own TTL and is cleared on
  success.
- OTP values are never logged in production (`ENV=production`); in other
  environments they are logged at Info level for development convenience.

### POST `/api/auth/verify-email`

```json
{ "email": "user@example.com", "otp": "123456" }
```

### POST `/api/auth/forgot-password`

```json
{ "email": "user@example.com" }
```

### POST `/api/auth/reset-password`

```json
{
  "email": "user@example.com",
  "otp": "123456",
  "new_password": "NewSecret1"
}
```

### POST `/api/auth/resend-verification`

```json
{ "email": "user@example.com" }
```

### POST `/api/auth/logout`

Clears cookie. Response: `{ "message": "Logged out successfully" }`.

---

## Profile & users (JWT required)

| Method | Path | Roles | Description |
|--------|------|-------|-------------|
| GET | `/api/protected/profile` | any auth | Current user |
| PUT | `/api/protected/change-password` | any auth | Change password |
| GET | `/api/protected/users` | admin+ | List users |
| GET | `/api/protected/users/:id` | admin+ | Get user |
| POST | `/api/protected/users` | admin+ | Create user |
| PUT | `/api/protected/users/:id` | admin+ | Update user |
| DELETE | `/api/protected/users/:id` | admin+ | Soft-delete user |

---

## Admin dashboards

| Method | Path | Roles |
|--------|------|-------|
| GET | `/api/protected/admin/dashboard` | admin+ — includes real entity `counts` |
| GET | `/api/protected/admin/users/stats` | admin+ |
| GET | `/api/protected/superadmin/dashboard` | super_admin |
| GET | `/api/protected/superadmin/system/info` | super_admin |

Admin dashboard `counts`: `faculties`, `courses`, `modules`, `classes`, `rooms`, `staff`, `timetables`.

---

## Generation settings (JWT + super_admin)

System-wide singleton for timetable generation engine options (solver-tunable
knobs). One settings row affects **every** future generation run — hence the
`superadmin` gating. If never configured, `GET` returns populated defaults
(`time_budget_sec: 30`, empty `soft_weights`) rather than erroring.

| Method | Path | Body |
|--------|------|------|
| GET | `/api/protected/superadmin/generation-settings` | — → `{ "settings": { "id", "time_budget_sec", "soft_weights", "created_at", "updated_at" } }` |
| PUT | `/api/protected/superadmin/generation-settings` | partial: `{ "time_budget_sec"?, "soft_weights"? }` |

Validation (server-side, rejects with `400`):

- `time_budget_sec`: finite, `> 0`, `<= 300`
- `soft_weights`: keys restricted to the allow-list
  `preferred_start_weight`, `session_spread_weight` (unknown keys are rejected
  with a message listing the allowed keys); values must be finite `>= 0`
- Omitted fields leave current values unchanged

Note: settings are stored but **not yet consumed** by `buildSolverRequest` —
wiring them into generation is ticket 136.c.

---

## Timetable domain (JWT + admin)

Base: `/api/protected/timetable`

### My staff profile (any authenticated user — own data only)

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/protected/me/staff` | `{ "staff": {...} }` or `404 {"error":"No staff profile is linked to your account","staff":null}` |

Resolves **server-side only** from the JWT `user_id` via `staffs.user_id` FK. Used to answer “which Staff record belongs to the currently logged-in user” without the client supplying a staff ID. Returns `404` (not `500`) when no Staff is linked — callers should treat `404`/`null` as “no staff profile”, not as a hard error.

### My timetable (any authenticated user — own data only)

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/protected/timetable/my` | `{ "timetables": [...] }` |

The Staff record is resolved **server-side only**, from the JWT user via the
`staffs.user_id` foreign key. Client-supplied `staff_id` values (query, body,
path) are ignored, so a `role=user` account can never read another staff
member's timetable. Returns `404` if no Staff profile is linked to the
account. Admins can still read any staff timetable via `/by-staff/:staff_id`.

List endpoints accept: `?limit=10&offset=0`.

### Faculties (UI: Departments)

| Method | Path | Body |
|--------|------|------|
| POST | `/faculties` | `{ "name", "description?", "hod_name?", "hod_phone?", "hod_email?" }` |
| GET | `/faculties` | `{ "faculties": [...] }` |
| GET | `/faculties/:id` | |
| PUT | `/faculties/:id` | partial |
| DELETE | `/faculties/:id` | |

### Courses (UI: Programs)

| Method | Path | Body |
|--------|------|------|
| POST | `/courses` | `{ "name", "faculty_id", "description?", "level?" }` |
| GET | `/courses` | |
| GET | `/courses/:id` | |
| PUT | `/courses/:id` | partial |
| DELETE | `/courses/:id` | |

### Modules

| Method | Path | Body |
|--------|------|------|
| POST | `/modules` | `{ "name", "code?", "course_id?", "credit_hours", "type", "requires_lab?", "semester?", "nta_level?" }` |
| GET | `/modules` | |
| GET | `/modules/:id` | |
| PUT | `/modules/:id` | partial (`clear_course` bool to null course_id) |
| DELETE | `/modules/:id` | |
| GET | `/modules/:id/staff` | staff assigned to module |

**`type`:** `core` \| `elective` \| `general_subject`  
`course_id` null for general subjects.

### Classes

| Method | Path | Body |
|--------|------|------|
| POST | `/classes` | `{ "name", "course_id", "year", "number_of_students", "academic_year?" }` |
| GET | `/classes` | |
| GET | `/classes/:id` | |
| PUT | `/classes/:id` | partial |
| DELETE | `/classes/:id` | |

`year`: year of study 1–6. `academic_year`: calendar string e.g. `2024/25`.

### Rooms

| Method | Path | Body |
|--------|------|------|
| POST | `/rooms` | `{ "name", "capacity", "features?", "sticky?", "allowed_courses?" }` |
| GET | `/rooms` | |
| GET | `/rooms/:id` | |
| PUT | `/rooms/:id` | partial |
| DELETE | `/rooms/:id` | |

`features` / `allowed_courses` accepted as **JSON strings**. Features shape:

```json
{
  "projector": true,
  "lab": false,
  "studio": false,
  "ac": true,
  "whiteboard": true,
  "computers": 0,
  "building": "A",
  "room_no": "101",
  "description": "...",
  "room_type": "lecture"
}
```

### Staff

| Method | Path | Body |
|--------|------|------|
| POST | `/staff` | `{ "name", "email", "faculty_id", "max_hours?", "preferences?", "rfid_id?", "phone_number?", "title?", "staff_type?", "user_id?" }` — `user_id` optionally links to an existing `User` (admin must supply explicit user ID; never auto-matched by email) |
| GET | `/staff` | |
| GET | `/staff/:id` | |
| PUT | `/staff/:id` | partial (including `user_id` to (re)link) |
| DELETE | `/staff/:id` | |
| POST | `/staff/:id/modules/:module_id` | assign |
| DELETE | `/staff/:id/modules/:module_id` | unassign |
| GET | `/staff/:id/modules` | list modules for staff |

### Subjects

| Method | Path | Body |
|--------|------|------|
| POST | `/subjects` | `{ "name", "credit_hours" }` |
| GET | `/subjects` | |
| GET | `/subjects/:id` | |
| PUT | `/subjects/:id` | partial |
| DELETE | `/subjects/:id` | |

### Timetable entries

| Method | Path | Notes |
|--------|------|-------|
| POST | `/generate` | `{ "class_id" }` — persist solution |
| POST | `/generate/preview` | dry-run, no DB writes |
| POST | `/` | manual entry |
| GET | `/:id` | single entry |
| PUT | `/:id` | partial update |
| DELETE | `/:id` | |
| GET | `/class/:class_id` | entries for class |
| GET | `/by-staff/:staff_id` | entries for staff (**not** `/staff/:id` — avoids CRUD clash) |
| GET | `/by-course/:course_id` | entries for all classes in a course (`[]` if none) |
| GET | `/validate` | stub message |

**Generate response**

```json
{
  "message": "Timetable generated successfully",
  "timetables": [],
  "count": 12,
  "status": "optimal",
  "violated_soft_constraints": [],
  "engine": "solver"
}
```

Infeasible: HTTP `422` with `unsat_reasons`. Conflicts on manual create: `409`.

**Manual create:** exactly one of `module_id` XOR `subject_id`.

---

## Error shape

```json
{ "error": "Invalid credentials" }
```

or

```json
{ "error": "Invalid request payload", "details": "..." }
```

---

## CORS

Env `CORS_ALLOWED_ORIGINS` (comma-separated). Defaults include `http://localhost:3000` and `http://localhost:5173`.
