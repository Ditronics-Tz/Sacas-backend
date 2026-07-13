# RBAC Audit — SACAS Backend + Frontend

**Date:** 2026-07-13  
**Status:** Backend middleware chain **PASS** (verified live + tests). Frontend hardened to trust JWT role claims.

---

## Intended policy (source of truth)

| Role | Access |
|------|--------|
| `user` | Own profile, change password, settings UI only |
| `administrator` | All of above + `/api/protected/users/*` + `/api/protected/admin/*` + all `/api/protected/timetable/*` |
| `super_admin` | All of above + `/api/protected/superadmin/*` |

Shared institutional data (faculties, rooms, staff, …) is **global once admin-gated** — not scoped per user. Correct: every admin sees all rooms. Bug would be: `role=user` can call those endpoints at all.

---

## Part 1 — Backend findings

### Middleware chain (from `routes.go`)

| Route group | Middleware chain | Intended | Status |
|-------------|------------------|----------|--------|
| `GET /api/health`, `/metrics`, `/csrf` | none (public) | public | PASS |
| `/api/auth/*`, `/api/otp/*` | rate limit | public + rate limit | PASS |
| `/api/protected/*` | JWT + ActiveUser | any authenticated active user | PASS |
| `GET/PUT /api/protected/profile`, `change-password` | (protected only) | any auth | PASS |
| `/api/protected/users/*` | JWT + ActiveUser + **AdminMiddleware** (per-route) | admin+ | PASS |
| `/api/protected/admin/*` | JWT + ActiveUser + **AdminMiddleware** (group) | admin+ | PASS |
| `/api/protected/superadmin/*` | JWT + ActiveUser + **SuperAdminMiddleware** | super_admin only | PASS |
| `/api/protected/timetable/*` (all faculties, courses, modules, classes, rooms, staff, subjects, generate, CRUD) | JWT + ActiveUser + **AdminMiddleware** (group `.Use`) | admin+ | PASS |

**Role source:** `JWTAuthMiddleware` parses signed JWT and `c.Set("role", claims["role"])`. `RoleMiddleware` / `AdminMiddleware` read **only** from Gin context (JWT claims), never from request body/query/header.

### Live probe (2026-07-13)

| Account | Role | `GET /api/protected/timetable/faculties` |
|---------|------|------------------------------------------|
| `lecturer@sacas.local` | `user` | **403** |
| `coordinator@sacas.local` | `administrator` | **200** |
| `admin@example.com` | `super_admin` | **200** |

### Root cause of “everyone sees everything” (if observed in UI)

Backend was **not** the primary leak for timetable APIs (group already has `AdminMiddleware`). Remaining risks addressed in this pass:

1. **Unsafe role type assertion** in `RoleMiddleware` (`role.(string)` could panic on unexpected claim types) — fixed with safe string conversion.
2. **Frontend role source of truth** — UI previously trusted only `user.role` in `localStorage`; now prefers **JWT claim `role`** so a stale/missing user object cannot open admin UI.
3. **Regression risk** — no automated matrix before; now covered by Go middleware tests + frontend `hasRole` / nav tests.

---

## Part 2 — Backend fixes applied

- `RoleMiddleware` / `RequireRole` factory: safe role string extraction; alias `RequireRole(...)`.
- Table-driven tests: user/admin/super_admin tokens against admin, superadmin, and timetable-group handlers.

---

## Part 3 — Frontend findings

| Route | Guard in App.js | Status (after fix) |
|-------|-----------------|-------------------|
| `/dashboard`, `/settings` | `RequireAuth` only | PASS (any auth) |
| All `/rooms/*`, `/classes/*`, `/modules/*`, `/staff/*`, `/programs/*`, `/departments/*`, `/allocations/*`, `/timetable`, `/preview` | `RequireAuth` + `AdminOnly` (`RequireRole`) | PASS |
| Public auth routes | none | PASS |

**Nav:** Sidebar already gated with `isAdmin()`; now `isAdmin` / `hasRole` read JWT claims first so a `role=user` session cannot see Timetable/Rooms/… items even if `user` blob is incomplete.

**Direct URL:** `RequireRole` shows “Insufficient permissions” (not blank page) for non-admin.

---

## Part 5 — Role × capability matrix (verified intent)

| Capability | `user` | `administrator` | `super_admin` |
|------------|--------|-----------------|---------------|
| Login / profile / settings | ✅ | ✅ | ✅ |
| Timetable domain API | ❌ 403 | ✅ | ✅ |
| Admin dashboard API | ❌ 403 | ✅ | ✅ |
| Superadmin system info | ❌ 403 | ❌ 403 | ✅ |
| Sidebar: admin sections | hidden | shown | shown |
| Direct `/rooms/view` URL | permissions screen | page | page |

---

## Adding a new admin route (checklist)

1. Register under `protected.Group("/timetable")` (already has `AdminMiddleware`) **or** attach `middlewares.RequireRole("administrator", "super_admin")`.
2. Wrap FE route in `<AdminOnly>` / `RequireRole`.
3. Hide nav item unless `isAdmin()`.
4. Add a test case that `role=user` gets 403.
