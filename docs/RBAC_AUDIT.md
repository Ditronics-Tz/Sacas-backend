# RBAC Audit — SACAS Backend

**Date:** 2026-07-13 (original) · **Revised:** 2026-09-30 (multi-tenancy + roles)
**Status:** Backend enforcement **PASS** (unit-tested). Frontend guidance in §6.

---

## 1. The model

Authorisation is now a **capability** model, not a role check. Roles are assigned
permissions in `internal/auth/permissions.go`, and routes ask for the capability
they need via `middlewares.RequirePermission(...)`. This matters because the
previous model (`AdminMiddleware` = "administrator or super_admin") could not
express the two coordinator roles, and because widening a role now happens in
one file instead of at every route.

### Roles

| Role | Scope | Purpose |
|---|---|---|
| `super_admin` | platform | Manages institutions themselves. Not bound to an institution. |
| `administrator` | institution | Institution admin. Top role inside one tenant. |
| `academic_coordinator` | institution | Runs the curriculum and the teaching timetable. |
| `exam_coordinator` | institution | Runs exams. Reads the structure; cannot rewrite the curriculum. |
| `user` | institution | Lecturer / staff. Read-only, plus own profile. |
| `institution_support` | institution | **Not assignable to a person.** Exists only inside a short-lived support token. |

### The three rules that matter

**1. A platform account has no institution-workspace access at all.**

`super_admin` holds *only* platform permissions. It holds no `faculty:read`, no
`user:read`, no `timetable:read`. This is deliberate: a super_admin is not bound
to an institution, so it has no tenant to scope a query with, and giving it
workspace permissions would reopen exactly the cross-tenant leak that the
tenancy work closed. Institution routes additionally run
`RequireInstitutionWorkspace()`, which fails closed with an explanatory 403.

Support needs to see inside a tenant, so that is granted deliberately, narrowly,
and on the record — see §4.

**2. A tenant admin cannot promote anyone.**

`auth.AssignableRoles` returns only `user` for any non-platform caller. An
institution admin can create and edit lecturers, but cannot mint another
administrator or a coordinator. Promotion is a platform decision. Without this,
a compromised tenant admin escalates inside its own tenant.

The platform `super_admin` can assign any real role, in any institution — this is
how institution admins are created. It still cannot assign
`institution_support`, for anyone.

**3. A `super_admin` must not be bound to an institution.**

A super_admin with an `institution_id` is a tenant user with platform reach: the
workspace gate refuses it, so it would be an account that can do almost nothing.
Creation and role-change both reject the combination at write time with a clear
message rather than leaving a broken account behind.

---

## 2. Role × endpoint matrix

✅ allowed · ❌ 403 · — not reachable (workspace/platform gate refuses first)

### Institution workspace — `/api/protected/timetable/*`

Gated by `RequireInstitutionWorkspace` then `RequirePermission`.

| Endpoint | Permission | `user` | `exam_coord` | `academic_coord` | `admin` | `super_admin` |
|---|---|---|---|---|---|---|
| `GET /faculties`, `/faculties/:id` | `faculty:read` | ✅ | ✅ | ✅ | ✅ | — |
| `POST/PUT/DELETE /faculties*` | `faculty:write` | ❌ | ❌ | ✅ | ✅ | — |
| `GET /courses*` | `course:read` | ✅ | ✅ | ✅ | ✅ | — |
| `POST/PUT/DELETE /courses*` | `course:write` | ❌ | ❌ | ✅ | ✅ | — |
| `GET /modules*` | `module:read` | ✅ | ✅ | ✅ | ✅ | — |
| `POST/PUT/DELETE /modules*` | `module:write` | ❌ | ❌ | ✅ | ✅ | — |
| `GET /classes*` | `class:read` | ✅ | ✅ | ✅ | ✅ | — |
| `POST/PUT/DELETE /classes*` | `class:write` | ❌ | ❌ | ✅ | ✅ | — |
| `GET /rooms*` | `room:read` | ✅ | ✅ | ✅ | ✅ | — |
| `POST/PUT/DELETE /rooms*` | `room:write` | ❌ | ❌ | ✅ | ✅ | — |
| `GET /staff`, `/staff/:id` | `staff:read` | ✅ | ✅ | ✅ | ✅ | — |
| `POST/PUT/DELETE /staff*` | `staff:write` | ❌ | ❌ | ✅ | ✅ | — |
| `GET /subjects*` | `subject:read` | ✅ | ✅ | ✅ | ✅ | — |
| `POST/PUT/DELETE /subjects*` | `subject:write` | ❌ | ❌ | ✅ | ✅ | — |
| `GET /modules/:id/staff`, `/staff/:id/modules` | `staff:read` | ✅ | ✅ | ✅ | ✅ | — |
| `POST/DELETE /staff/:id/modules/:module_id` | `staff:write` | ❌ | ❌ | ✅ | ✅ | — |
| `POST /generate/preview` | `timetable:preview` | ❌ | ✅ | ✅ | ✅ | — |
| `POST /generate`, `POST /` | `timetable:generate` | ❌ | ✅ | ✅ | ✅ | — |
| `GET /:id`, `/class/:id`, `/by-staff/:id`, `/by-course/:id` | `timetable:read` | ✅ | ✅ | ✅ | ✅ | — |
| `PUT /:id`, `DELETE /:id` | `timetable:override` | ❌ | ❌ | ❌ | ✅ | — |

`timetable:preview` is separate from `timetable:generate` so a coordinator can
experiment without overwriting a published timetable.
`timetable:override` (editing individual entries after the fact) is admin-only:
it is the escape hatch that can break a generated schedule, so it is the most
guarded write in the workspace.

### Institution administration

| Endpoint | Permission | `user` | `academic_coord` | `exam_coord` | `admin` | `super_admin` |
|---|---|---|---|---|---|---|
| `GET /protected/users*` | `user:read` | ❌ | ✅ | ❌ | ✅ | — |
| `POST /protected/users` | `user:write` | ❌ | ❌ | ❌ | ✅ | — |
| `PUT /protected/users/:id` | `user:write` | ❌ | ❌ | ❌ | ✅ | — |
| `DELETE /protected/users/:id` | `user:delete` | ❌ | ❌ | ❌ | ✅ | — |
| `GET /protected/admin/dashboard` | `admin:stats` | ❌ | ❌ | ❌ | ✅ | — |
| `GET /protected/admin/users/stats` | `admin:stats` | ❌ | ❌ | ❌ | ✅ | — |
| `GET /protected/audit` | `admin:stats` | ❌ | ❌ | ❌ | ✅ | — |
| `GET /protected/admin/metrics` | `admin:stats` | ❌ | ❌ | ❌ | ✅ | — |

`academic_coordinator` gets `user:read` deliberately: it needs the user list to
resolve a staff member to a login when allocating teaching. It has no
`user:write`, so it still cannot create or delete anyone.

### Platform — `/api/protected/superadmin/*`

Gated by `RequirePlatformWorkspace`, which requires the role to be `super_admin`
**and** the account to be unbound to an institution.

| Endpoint | Permission | `super_admin` (platform) | `super_admin` (institution-bound) | everyone else |
|---|---|---|---|---|
| `GET /superadmin/dashboard` | platform workspace | ✅ | ❌ | ❌ |
| `GET /superadmin/system/info` | platform workspace | ✅ | ❌ | ❌ |
| `GET /institutions` | `institution:read` | ✅ | ❌ | ❌ |
| `GET /institutions/:id` | `institution:read` | ✅ | ❌ | ❌ |
| `POST /institutions` | `institution:write` | ✅ | ❌ | ❌ |
| `PUT /institutions/:id` | `institution:write` | ✅ | ❌ | ❌ |
| `DELETE /institutions/:id` | `institution:delete` | ✅ | ❌ | ❌ |
| `POST /institutions/:id/impersonate` | `institution:impersonate` | ✅ | ❌ | ❌ |
| `GET/PUT /superadmin/generation-settings` | `platform:settings` | ✅ | ❌ | ❌ |
| `GET /superadmin/audit*` | `audit:read` | ✅ | ❌ | ❌ |

### Self-service — any authenticated account

| Endpoint | `user` | `exam_coord` | `academic_coord` | `admin` | `super_admin` | support |
|---|---|---|---|---|---|---|
| `GET /protected/profile` | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| `PUT /protected/change-password` | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| `GET /protected/me/staff` | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| `GET /protected/timetable/my` | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| `GET /protected/institution/me` | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| `GET /protected/me/permissions` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

A support session is refused self-service too: it must not be able to change the
operator's own account.

---

## 3. Where the role comes from

`JWTAuthMiddleware` verifies the token and sets the claims.
`TenantMiddleware` then **overwrites `user_id`, `role`, and `email` from the
user's database row** and resolves `institution_id`. A role change, a
deactivation, or a suspension therefore takes effect on the very next request
rather than at token expiry.

`RequirePermission` reads the role from that context only. A `X-Role` header, a
`role` query parameter, or a body field cannot influence it — covered by
`TestRequirePermission_ignoresClientSuppliedRole`.

A missing role fails closed (`403`), on the assumption that a chain which did not
resolve a role is misconfigured.

---

## 4. Support access (impersonation)

`POST /api/protected/superadmin/institutions/:id/impersonate` is the **only** way
a platform operator can see inside an institution's workspace.

| Constraint | How it is enforced |
|---|---|
| Read-only | The token carries `institution_support`, a role with no write permission. Enforced in the permission map, not by hiding buttons. |
| Short-lived | `IM_PERSONATION_TTL_MINUTES` (default 15). A non-positive value falls back to the default rather than issuing an already-expired token. |
| One institution | The institution is inside the signed token; the tenant middleware resolves it and re-checks the institution is still active. |
| Always audited | The `impersonate.start` row is written **before** the token is returned, so a live session is never unrecorded. |
| Not escalatable | The support role holds no `institution:impersonate`, so a support session cannot open another. It also has no `user:write`, so it cannot create a platform account. |
| Off by default | `IM_PERSONATION_ENABLED=false` unless explicitly set. Support access is an operational decision, not a default. |

A normal login token is **not** accepted as a support token: `ParseSupportToken`
requires the `support` flag, the support role, `read_only: true`, and an
institution. Without all four checks, a compromised low-privilege token would
gain read access to an arbitrary institution.

`POST .../impersonate/end` records the end. Tokens are stateless and cannot be
revoked server-side, so a session with a start and no end is how an abandoned
session is spotted in the trail.

**Known limitation:** `IM_PERSONATION_MAX_CONCURRENT` is counted in-process, so
the cap is per API instance. A multi-instance deployment should move it to shared
state; until then the cap errs toward *allowing* a session rather than locking an
operator out.

---

## 5. Audit log

`audit_logs` is append-only. There is deliberately no `deleted_at` and no update
or delete path in the repository: a trail that can be edited afterwards cannot
answer "who changed this, and when".

| Column | Purpose |
|---|---|
| `recorded_at` | When the event happened (not when it was written) |
| `actor_id`, `actor_email`, `actor_role` | Who did it. `0` = the system. |
| `institution_id` | Which tenant. **NULL = platform action** — this is how the two are told apart. |
| `action` | e.g. `role.change`, `impersonate.start` |
| `outcome` | `success` / `denied` / `failure` |
| `target`, `target_type`, `target_id` | What was acted on |
| `detail` | JSON context, e.g. `{"from":"user","to":"administrator"}` |
| `ip_address`, `user_agent` | Abuse investigation. Spoofable behind a proxy — a hint, not proof. |

### Actions recorded

`role.change`, `user.create`, `user.update`, `user.delete`, `user.suspend`,
`user.activate`, `password.reset`, `role.self_elevate`,
`institution.create`, `institution.update`, `institution.suspend`,
`institution.delete`, `institution.plan_change`, `impersonate.start`,
`impersonate.end`, `timetable.generate`, `timetable.publish`,
`timetable.approve`, `exam.publish`, `exam.approve`, `data.import`.

`role.change` records the **before and after** pair, so "who was promoted to
what" is a direct query rather than an inference.

**Refusals are recorded too**, as `outcome: denied`. A stream of denied
impersonation or denied role escalation is exactly the signal a support engineer
looks for, and it is invisible if only successes are kept.

### Audit failures do not fail the request

An audit write that fails is logged at `ERROR` and the business operation is
allowed to succeed. This is a deliberate trade-off: the operation has already
happened, and reporting it as failed afterwards would leave the caller believing
it did not occur. Losing the row is bad, but failing a committed write is worse,
and the loud log makes the gap visible. It is not a silent failure.

### Reading the trail

- `GET /api/protected/superadmin/audit` — platform-wide, or `?institution_id=N`.
- `GET /api/protected/superadmin/audit/target/:type/:id` — one object's history,
  oldest first.
- `GET /api/protected/audit` — an institution's own trail only.

Filters: `institution_id`, `actor_id`, `action`, `outcome`, `target_type`,
`target_id`, `from`, `to` (RFC3339), `limit`, `offset`. An unknown `action` is
rejected with the valid list rather than returning an empty page that looks like
"no such activity happened".

---

## 6. Frontend guidance

Call `GET /api/protected/me/permissions` after login and gate the UI on the
returned list, rather than hardcoding a role check. The server is then the single
source of truth and the two cannot drift.

```json
{
  "role": "academic_coordinator",
  "permissions": ["class:read", "course:read", "..."],
  "assignable_roles": ["user"],
  "institution_id": 2,
  "is_platform_account": false,
  "is_support_session": false,
  "read_only": false,
  "workspace": "institution"
}
```

- `workspace` is `platform`, `institution`, or `support` — use it to decide the
  whole nav shape, not individual items.
- `assignable_roles` is the **server's** answer to "which roles may this user
  hand out". Populate the role dropdown from it. A tenant admin will see only
  `user`, which is the UI half of the anti-escalation rule; hiding the other
  options in the frontend alone would be cosmetic.
- `read_only` means "cannot change anything anybody else can see", **not** "can
  do nothing": a lecturer can still change their own password. Use it for a
  global banner, not to disable the whole UI.
- `is_support_session: true` means disable every editing affordance. The server
  enforces it, so this is UX, not security.
- A `403` body names the *required* permission but never the caller's own
  permission list, so do not expect to reconstruct the caller's access from an
  error — use `/me/permissions`.

Existing `isAdmin()` / `hasRole()` checks should be replaced by permission
checks. `role=user` must not reach any workspace route, and a `super_admin` must
not appear to have institution screens at all.

---

## 7. Adding a route or a role

**New route:** attach `middlewares.RequirePermission(auth.PermX)`. Inside an
institution group, `RequireInstitutionWorkspace()` is already applied. Do not use
`RequireRole` for new code — it names roles rather than capabilities and drifts.

**New role:** add it to `models.AllRoles` **and** to `rolePermissions` in
`internal/auth/permissions.go`. The package validates both at `init`, so a role
with no entry — or an entry granting a misspelled permission — panics at boot
rather than silently denying access at runtime.

**New write permission:** add it to `writePermissions` as well. Otherwise
`IsReadOnly` will keep reporting a role that can write as read-only. The
`TestEveryWritePermissionIsHeldByAtLeastOneRole` and `TestIsReadOnly` tests fail
until both are done.

**Tests:** the role × permission matrix in `internal/middlewares/rbac_test.go` is
generated from the permission map, so it does not need editing when permissions
change — but it will tell you immediately if a change broke an intended policy.
Add a case to `internal/auth/permissions_test.go` for any new invariant.

---

## 8. Test coverage

| File | Covers |
|---|---|
| `internal/auth/permissions_test.go` | Map consistency, coordinator domain separation, admin superset, read-only classification, assignable roles, support role not assignable |
| `internal/middlewares/rbac_test.go` | Generated role × permission matrix, super_admin has no workspace access, support is read-only, workspace/platform gates, header spoofing, fail-closed without a role |
| `internal/services/audit_test.go` | Actor/institution capture, platform vs tenant, denied outcomes, failed write does not panic, context coercions |
| `internal/services/impersonation_test.go` | Disabled by default, read-only session, TTL, refuses non-platform callers, unknown institution, concurrency cap, refusals audited |
| `internal/services/support_token_test.go` | Round trip, expiry, garbage, a normal token is not a support token, write-intent rejected, foreign signature |
| `internal/controllers/user_roles_test.go` | Tenant admin can only create lecturers, platform creates institution admins, super_admin must not be bound to an institution, role change audited before/after, `/me/permissions` shapes per role |

---

## Appendix — earlier work (retained for context)

**Part 2b — auth hardening (2026-09-02)**

1. OTP values are never logged in production.
2. Constant-time OTP comparison via `pkg/security.SecureCompare`.
3. Per-account OTP attempt lockout in Redis (`otp_attempts:{purpose}:{email}`),
   returning 429 after `OTP_MAX_ATTEMPTS` (default 5).
4. Password policy: ≥8 chars with an uppercase, a lowercase, and a digit.
5. Logout sets the same `secure` cookie attribute as login.
6. Token-in-body is deliberate and gated by `AUTH_RETURN_TOKEN_IN_BODY`.

**Tenant isolation (2026-09-30)** — see `docs/api-contract.md` § Multi-tenancy.
The previous line in this document, "shared institutional data is global once
admin-gated", is **no longer true**: all timetable data is per-institution, and a
cross-tenant access returns 404 rather than 403 so the API does not confirm
another tenant's records exist.
