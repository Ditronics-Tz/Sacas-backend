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

## Roles and permissions

Authorisation is a capability model. `GET /api/protected/me/permissions` returns
the caller's exact permission list, and **the UI should gate on that** rather
than hardcoding role checks — the server stays the single source of truth.

### GET `/api/protected/me/permissions`

Any authenticated account. No query parameters. Call it once after login.

```json
{
  "role": "academic_coordinator",
  "permissions": [
    "class:read", "class:write", "course:read", "course:write",
    "faculty:read", "faculty:write", "module:read", "module:write",
    "room:read", "room:write", "staff:read", "staff:write",
    "subject:read", "subject:write", "timetable:generate",
    "timetable:preview", "timetable:publish", "timetable:read",
    "profile:read", "profile:update", "password:set", "user:read"
  ],
  "assignable_roles": ["user"],
  "institution_id": 2,
  "is_platform_account": false,
  "is_support_session": false,
  "read_only": false,
  "workspace": "institution"
}
```

| Field | Meaning |
|---|---|
| `permissions` | Everything this session may do. Gate UI affordances on these. |
| `assignable_roles` | Which roles this user may hand out. **Populate the role dropdown from this** — a tenant admin sees only `user`, which is the UI half of the anti-escalation rule. |
| `workspace` | `platform`, `institution`, or `support`. Use it to pick the nav shape. |
| `is_platform_account` | A `super_admin` not bound to an institution. |
| `is_support_session` | A read-only support session; disable all editing. |
| `read_only` | Cannot change anything anybody else can see. **Not** "can do nothing" — a lecturer can still change their own password. Use for a global banner only. |
| `institution_id` | `null` for a platform account. |

### Roles

| Role | Can |
|---|---|
| `super_admin` | Manage institutions, roles, platform settings, audit trail. **No** institution-workspace access. |
| `administrator` | Full control inside one institution, including its users. |
| `academic_coordinator` | Curriculum and teaching timetable. Reads users (to resolve staff), cannot create them. |
| `exam_coordinator` | Exams. Reads the structure to schedule them, cannot rewrite the curriculum. |
| `user` | Own profile and read-only view of the structure. |

A `super_admin` is **not** bound to an institution, so it cannot reach any
institution's data — a request to an institution workspace returns `403` with
`"reason": "platform accounts are not bound to an institution"`. Support access
is granted separately, below.

### Role assignment

- A platform `super_admin` may create any role in any institution. This is how
  institution admins come to exist.
- An institution admin may assign **only** `user`. Promoting anyone requires the
  platform.
- A `super_admin` cannot be bound to an institution; the request is rejected with
  `400` rather than creating an account the workspace gate would refuse.
- The support role is never assignable.

A refused attempt returns `403` and is written to the audit log as
`outcome: denied`.

### 403 body

```json
{
  "error": "Insufficient permissions",
  "your_role": "user",
  "required": ["user:write"]
}
```

The **required** permission is included so the UI can explain itself. Your own
permission list is not, so you cannot reconstruct the policy from an error — use
`/me/permissions`.

### Support access (impersonation)

`POST /api/protected/superadmin/institutions/:id/impersonate` — platform only.
Returns a **read-only, short-lived, audited** session token:

```json
{
  "message": "Read-only support session created. Every action taken with this token is recorded in the audit log.",
  "session": {
    "token": "…",
    "institution_id": 2,
    "institution_name": "Dar es Salaam University",
    "role": "institution_support",
    "expires_at": "2026-09-30T15:10:00Z",
    "read_only": true,
    "allowed_permissions": ["class:read", "course:read", "…"]
  }
}
```

Use `session.token` as the bearer token to browse that institution read-only.
Disabled unless `IM_PERSONATION_ENABLED=true`. `POST .../impersonate/end` records
the end of the session.

---

## Audit log

`GET /api/protected/audit` — the caller's own institution's trail.
`GET /api/protected/superadmin/audit` — platform-wide, or `?institution_id=N`.
`GET /api/protected/superadmin/audit/target/:type/:id` — one object's history,
oldest first.

Filters: `institution_id`, `actor_id`, `action`, `outcome`, `target_type`,
`target_id`, `from`, `to` (RFC3339), `limit`, `offset`.

```json
{
  "entries": [
    {
      "id": 412,
      "recorded_at": "2026-09-30T14:02:11Z",
      "actor_id": 1,
      "actor_email": "root@sacas.co.tz",
      "actor_role": "super_admin",
      "institution_id": null,
      "action": "role.change",
      "outcome": "success",
      "target": "coordinator@dstu.ac.tz",
      "target_id": "42",
      "target_type": "user",
      "detail": "{\"from\":\"user\",\"to\":\"academic_coordinator\"}",
      "ip_address": "197.157.2.3",
      "user_agent": "Mozilla/5.0 …"
    }
  ],
  "total": 1,
  "limit": 10,
  "offset": 0
}
```

`institution_id: null` means a **platform** action. `outcome` is `success`,
`denied`, or `failure` — refusals are recorded too, so a run of denied
impersonation or escalation attempts is visible. The trail is append-only.

---

## Multi-tenancy

Every timetable-domain record belongs to exactly one institution. The tenant is
resolved **server-side on every request** from the authenticated user's own
database row — the client never sends an `institution_id` on normal routes, and
one is ignored if it does.

What this means for the frontend:

- **Log in, then use the API normally.** There is no institution switcher and no
  tenant header. The signed-in user's account already determines what is
  visible.
- **A record from another institution returns `404`, not `403`.** This is
  deliberate: a `403` would confirm that another institution's record exists.
  So "not found" and "belongs to someone else" are the same response, and the
  UI should just render an empty/not-found state.
- **Institution state gates access.** A `pending`, `suspended`, or
  trial-expired institution cannot sign anyone in; every protected route
  answers `403` with a `message` explaining why.
- **A role change or deactivation takes effect immediately**, not at token
  expiry — the role is re-read from the database on each request, so there is no
  need to force a re-login after an admin changes a user's role.
- **`super_admin` with no institution is a platform operator** and sees all
  institutions. A `super_admin` assigned to an institution is scoped to it like
  any other tenant.

Every response body for a domain record now includes `institution_id`, so the
UI can label or filter without a second call.

### GET `/api/protected/institution/me`

Any authenticated user. Returns the caller's own institution.

```json
{
  "institution": {
    "id": 2,
    "name": "Dar es Salaam University",
    "slug": "dar-es-salaam-university",
    "type": "university",
    "region": "Dar es Salaam",
    "country": "Tanzania",
    "email": "registry@dstu.ac.tz",
    "phone": "+255212345678",
    "address": "…",
    "logo_url": "…",
    "status": "active",
    "plan": "pro",
    "trial_ends_at": null,
    "created_at": "2026-09-30T09:00:00Z",
    "updated_at": "2026-09-30T09:00:00Z"
  },
  "scope": "institution"
}
```

`type` is one of `college`, `university`, `school`, `institute`.
`status` is one of `pending`, `active`, `suspended`.
`plan` is one of `free`, `pro`, `enterprise`.

A platform operator (`super_admin` with no institution) gets:

```json
{ "institution": null, "scope": "platform", "message": "This account is a platform administrator and is not bound to a single institution" }
```

### Institutions management (platform super_admin only)

Gated on `super_admin` **and** on having no institution of their own. A tenant's
own admin cannot reach these.

| Method | Path |
| --- | --- |
| `POST` | `/api/protected/superadmin/institutions` |
| `GET` | `/api/protected/superadmin/institutions` |
| `GET` | `/api/protected/superadmin/institutions/:id` |
| `PUT` | `/api/protected/superadmin/institutions/:id` |
| `DELETE` | `/api/protected/superadmin/institutions/:id` |

`POST` body: `name` and `type` are required. `slug` is optional and derived from
`name` when omitted. `status` defaults to **`pending`** — a new institution
cannot sign anyone in until it is explicitly activated. `plan` defaults to
`free` and `country` to `Tanzania`.

```json
{ "name": "Dar es Salaam University", "type": "university", "region": "Dar es Salaam", "email": "registry@dstu.ac.tz" }
```

`GET` supports `?status=pending|active|suspended`, plus `limit`/`offset`.
`GET /:id` also returns a `user_count`.

`DELETE` on the default institution (id `1`) is refused with `400` — it is the
backfill target for existing data, so it must be suspended rather than removed.

### Generation settings are layered

`GET`/`PUT /api/protected/superadmin/generation-settings` now operate on a
target institution:

- A platform super_admin with no `institution_id` reads/writes the **platform
  default**.
- Pass `?institution_id=N` (GET) or `"institution_id": N` (PUT) to read/write a
  specific tenant's **override**.
- An institution admin (not platform) is pinned to their own institution: a
  `institution_id` naming another tenant is rejected with `403`.

An institution with no override inherits the platform values, so the response
always shows the **effective** settings. `PUT` with `{"inherit_platform": true}`
drops the override and returns to inheritance. Responses carry `institution_id`
and `is_platform_row`.

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
| GET | `/api/protected/institution/me` | any auth | Caller's own institution |
| GET | `/api/protected/users` | admin+ | List users (own institution) |
| GET | `/api/protected/users/:id` | admin+ | Get user (own institution) |
| POST | `/api/protected/users` | admin+ | Create user |
| PUT | `/api/protected/users/:id` | admin+ | Update user (own institution) |
| DELETE | `/api/protected/users/:id` | admin+ | Soft-delete user (own institution) |

User listings, reads, updates, and deletes are scoped to the caller's
institution. Platform `super_admin` accounts (`institution_id` null) are **not**
included in a tenant's listing, and a tenant admin cannot reach them.

`POST /users` creates the user in the caller's own institution. An
`institution_id` in the body is honoured only when a **platform** super_admin
sends it; for anyone else it is ignored and the user lands in the caller's
institution.

---

## Admin dashboards

| Method | Path | Roles |
|--------|------|-------|
| GET | `/api/protected/admin/dashboard` | admin+ — real entity `counts` |
| GET | `/api/protected/admin/users/stats` | admin+ |
| GET | `/api/protected/superadmin/dashboard` | super_admin — adds `institutions` counts |
| GET | `/api/protected/superadmin/system/info` | super_admin |

Admin dashboard `counts`: `faculties`, `courses`, `modules`, `classes`, `rooms`,
`staff`, `timetables`. These are **scoped to the caller's institution**; a
platform super_admin sees platform-wide totals. The body includes
`institution_id` for a tenant, or `"scope": "platform"` for a platform operator,
so the UI can label the numbers.

Superadmin dashboard adds `institutions: { total, active, pending, suspended }`.

---

## Generation settings (JWT + super_admin)

Settings are **layered**: a platform-wide default row plus an optional
per-institution override. The API always returns the *effective* settings for the
target, so the UI never has to merge two responses itself. If nothing is
configured anywhere, `GET` returns populated defaults (`time_budget_sec: 30`,
empty `soft_weights`) rather than erroring.

See [Multi-tenancy](#multi-tenancy) above for how the target institution is
chosen and how to clear an override.

| Method | Path | Body |
|--------|------|------|
| GET | `/api/protected/superadmin/generation-settings` | — (optional `?institution_id=N`) → `{ "settings": { "id", "institution_id", "time_budget_sec", "soft_weights", "created_at", "updated_at" }, "institution_id", "is_platform_row" }` |
| PUT | `/api/protected/superadmin/generation-settings` | partial: `{ "time_budget_sec"?, "soft_weights"?, "institution_id"?, "inherit_platform"? }` |

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

List endpoints accept: `?limit=10&offset=0`. `limit` is capped server-side, so an
oversized value is clamped rather than honoured.

### Per-institution uniqueness

Names and codes are unique **within an institution**, not globally, and only
among non-deleted rows (soft-deleting frees the name for reuse). Matching is
case-insensitive.

| Entity | Unique per institution |
| --- | --- |
| Faculty | `name` |
| Course | `name` |
| Class | `name` |
| Module | `name`, and `code` (when non-empty) |
| Room | `name` |
| Subject | `name` |
| Staff | `email` |
| Institution | `slug` (platform-wide, not per-institution) |

`User.email` is the exception: it stays **globally unique**, because it is the
login identifier.

A collision surfaces as a `500` from the unique index rather than a friendly
`409` on every path — the staff create/update endpoints are the exception and do
return `409` for a duplicate email.

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

A `course_id` must belong to the caller's institution, on both create and
update; a cross-institution `course_id` is rejected with `400`. A `code` is
unique per institution (case-insensitively, among non-deleted rows).

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
| PUT | `/staff/:id` | partial. `user_id` (re)links the staff record; `clear_user_id: true` unlinks it. The link is written through a dedicated path, so an ordinary profile update can never re-point it |
| DELETE | `/staff/:id` | |
| POST | `/staff/:id/modules/:module_id` | assign |
| DELETE | `/staff/:id/modules/:module_id` | unassign |
| GET | `/staff/:id/modules` | list modules for staff |

A staff `email` is unique **per institution**, not globally — the same address
may exist at two institutions. Creating one that already exists in the caller's
institution returns `409`.

Linking a staff record to a `user_id` requires that user to belong to the same
institution; a cross-institution `user_id` is rejected with `400`. A user can be
linked to at most one staff record.

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

**Generation is tenant-scoped.** The `class_id` must belong to the caller's
institution; otherwise `404` (not a generation error). The engine reads only
that institution's modules, staff, rooms, and general subjects, so a generated
timetable can never reference another campus's rooms or lecturers, and a room
that is busy at another institution does not block a slot here.

Referenced records on manual create/update must also be in the caller's
institution — `class_id`, `staff_id`, and `room_id` each return `404` when they
belong to another institution.

---

## Error shape

```json
{ "error": "Invalid credentials" }
```

or

```json
{ "error": "Invalid request payload", "details": "..." }
```

A blocked institution adds context so the UI can explain itself:

```json
{ "error": "Institution is not active", "status": "suspended", "message": "This institution's account is suspended. Contact the platform administrator." }
```

---

## CORS

Env `CORS_ALLOWED_ORIGINS` (comma-separated). Defaults include `http://localhost:3000` and `http://localhost:5173`.
