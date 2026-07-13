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
  "new_password": "newsecret"
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

## Timetable domain (JWT + admin)

Base: `/api/protected/timetable`

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
| POST | `/staff` | `{ "name", "email", "faculty_id", "max_hours?", "preferences?", "rfid_id?", "phone_number?", "title?", "staff_type?" }` |
| GET | `/staff` | |
| GET | `/staff/:id` | |
| PUT | `/staff/:id` | partial |
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
