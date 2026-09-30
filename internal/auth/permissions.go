// Package auth holds the role → permission model.
//
// It is the single source of truth for what a role may do. Route handlers ask
// for a permission (middlewares.RequirePermission) rather than naming roles, so
// widening a role is a one-line change here and cannot drift between endpoints.
package auth

import (
	"sort"

	"go_boilerplate/internal/models"
)

// Permission is a single capability. Values are stable strings because the
// frontend reads them from GET /api/protected/me/permissions to decide which
// nav items and buttons to render.
type Permission string

const (
	// --- Self-service (every authenticated user) -------------------------
	// PermProfileRead / PermProfileUpdate cover the caller's own account and
	// own timetable, which are resolved from the session and never by ID.
	PermProfileRead   Permission = "profile:read"
	PermProfileUpdate Permission = "profile:update"
	PermPasswordSet   Permission = "password:set"

	// --- Academic structure (read) --------------------------------------
	PermFacultyRead   Permission = "faculty:read"
	PermCourseRead    Permission = "course:read"
	PermModuleRead    Permission = "module:read"
	PermClassRead     Permission = "class:read"
	PermRoomRead      Permission = "room:read"
	PermSubjectRead   Permission = "subject:read"
	PermStaffRead     Permission = "staff:read"
	PermTimetableRead Permission = "timetable:read"

	// --- Academic structure (write) -------------------------------------
	// The academic coordinator owns the teaching timetable end to end.
	PermFacultyWrite Permission = "faculty:write"
	PermCourseWrite  Permission = "course:write"
	PermModuleWrite  Permission = "module:write"
	PermClassWrite   Permission = "class:write"
	PermRoomWrite    Permission = "room:write"
	PermSubjectWrite Permission = "subject:write"
	PermStaffWrite   Permission = "staff:write"

	// --- Timetable generation -------------------------------------------
	// Preview is separated from commit so a coordinator can experiment without
	// overwriting a published timetable.
	PermTimetablePreview  Permission = "timetable:preview"
	PermTimetableGenerate Permission = "timetable:generate"
	PermTimetablePublish  Permission = "timetable:publish"
	PermTimetableApprove  Permission = "timetable:approve"
	PermTimetableOverride Permission = "timetable:override"

	// --- Exams -----------------------------------------------------------
	PermExamRead     Permission = "exam:read"
	PermExamWrite    Permission = "exam:write"
	PermExamSchedule Permission = "exam:schedule"
	PermExamPublish  Permission = "exam:publish"
	PermExamApprove  Permission = "exam:approve"

	// --- Institution administration --------------------------------------
	PermUserRead   Permission = "user:read"
	PermUserWrite  Permission = "user:write"
	PermUserDelete Permission = "user:delete"
	PermAdminStats Permission = "admin:stats"
	PermStaffLink  Permission = "staff:link"
	PermDataImport Permission = "data:import"
	// PermSettingsWrite is the institution's own generation-settings override.
	PermSettingsWrite Permission = "settings:write"

	// --- Support access (read-only) -------------------------------------
	// PermSupportRead is the only permission a support session holds. It grants
	// visibility of an institution's data and nothing else.
	PermSupportRead Permission = "support:read"

	// --- Platform administration -----------------------------------------
	// These are the only permissions a super_admin holds, and they act across
	// institutions rather than within one.
	PermInstitutionRead   Permission = "institution:read"
	PermInstitutionWrite  Permission = "institution:write"
	PermInstitutionDelete Permission = "institution:delete"
	PermPlatformSettings  Permission = "platform:settings"
	PermUserRoleWrite     Permission = "user:role:write"
	PermAuditRead         Permission = "audit:read"
	// PermImpersonate grants read-only support access into an institution.
	PermImpersonate Permission = "institution:impersonate"
)

// allPermissions is the union of every permission the system defines. Used to
// validate that no permission string in the map below is misspelled, which
// would otherwise silently deny access.
var allPermissions = []Permission{
	PermProfileRead, PermProfileUpdate, PermPasswordSet,
	PermFacultyRead, PermCourseRead, PermModuleRead, PermClassRead,
	PermRoomRead, PermSubjectRead, PermStaffRead, PermTimetableRead,
	PermFacultyWrite, PermCourseWrite, PermModuleWrite, PermClassWrite,
	PermRoomWrite, PermSubjectWrite, PermStaffWrite,
	PermTimetablePreview, PermTimetableGenerate, PermTimetablePublish,
	PermTimetableApprove, PermTimetableOverride,
	PermExamRead, PermExamWrite, PermExamSchedule, PermExamPublish, PermExamApprove,
	PermUserRead, PermUserWrite, PermUserDelete, PermAdminStats, PermStaffLink,
	PermDataImport, PermSettingsWrite,
	PermInstitutionRead, PermInstitutionWrite, PermInstitutionDelete,
	PermPlatformSettings, PermUserRoleWrite, PermAuditRead, PermImpersonate,
	PermSupportRead,
}

// rolePermissions is the role → permission map. This is the file to edit when
// a role's capabilities change.
//
// Design rules encoded here:
//
//   - super_admin holds ONLY platform permissions. It deliberately does not
//     hold the institution workspace permissions, because a super_admin is not
//     bound to an institution. Giving it them would let a platform operator
//     read and write tenant data with no tenant context, which is exactly the
//     leak the tenancy work closed. Support access to an institution is granted
//     through explicit, audited impersonation instead.
//   - academic_coordinator gets the full academic-structure surface and
//     timetable generation, but no user administration and no exam rights.
//   - exam_coordinator gets exams and the read-only academic surface needed to
//     build an exam timetable, but cannot change the curriculum.
//   - user gets self-service plus read-only visibility of the academic
//     structure, so a lecturer can see their own context without being able to
//     edit the catalogue.
var rolePermissions = map[models.UserRole][]Permission{
	models.RoleUser: {
		PermProfileRead, PermProfileUpdate, PermPasswordSet,
		// A lecturer may read the structure they teach against, but cannot
		// change it. Individual records are still filtered by tenant.
		PermFacultyRead, PermCourseRead, PermModuleRead, PermClassRead,
		PermRoomRead, PermSubjectRead, PermStaffRead, PermTimetableRead,
	},

	models.RoleExamCoord: {
		PermProfileRead, PermProfileUpdate, PermPasswordSet,
		PermFacultyRead, PermCourseRead, PermModuleRead, PermClassRead,
		PermRoomRead, PermSubjectRead, PermStaffRead, PermTimetableRead,
		PermExamRead, PermExamWrite, PermExamSchedule, PermExamPublish,
		PermTimetablePreview, PermTimetableGenerate,
	},

	models.RoleAcademicCoord: {
		PermProfileRead, PermProfileUpdate, PermPasswordSet,
		PermFacultyRead, PermCourseRead, PermModuleRead, PermClassRead,
		PermRoomRead, PermSubjectRead, PermStaffRead, PermTimetableRead,
		PermFacultyWrite, PermCourseWrite, PermModuleWrite, PermClassWrite,
		PermRoomWrite, PermSubjectWrite, PermStaffWrite,
		PermTimetablePreview, PermTimetableGenerate, PermTimetablePublish,
		PermDataImport, PermStaffLink,
		// A coordinator can see their own institution's user list in order to
		// resolve a staff member to a login, but cannot create or delete one.
		PermUserRead,
	},

	models.RoleAdmin: {
		PermProfileRead, PermProfileUpdate, PermPasswordSet,
		PermFacultyRead, PermCourseRead, PermModuleRead, PermClassRead,
		PermRoomRead, PermSubjectRead, PermStaffRead, PermTimetableRead,
		PermFacultyWrite, PermCourseWrite, PermModuleWrite, PermClassWrite,
		PermRoomWrite, PermSubjectWrite, PermStaffWrite,
		PermTimetablePreview, PermTimetableGenerate, PermTimetablePublish,
		PermTimetableOverride,
		// The institution admin is the top role inside a tenant, so it holds
		// every exam capability a coordinator has. Keeping the admin a strict
		// superset means adding a coordinator capability never accidentally
		// reduces what an existing admin can do.
		PermExamRead, PermExamWrite, PermExamSchedule, PermExamPublish, PermExamApprove,
		PermUserRead, PermUserWrite, PermUserDelete, PermAdminStats,
		PermStaffLink, PermDataImport, PermSettingsWrite,
		// The institution admin approves a timetable the coordinator produced.
		PermTimetableApprove,
	},

	models.RoleSuperAdmin: {
		// Platform scope only. See the design note above.
		PermProfileRead, PermProfileUpdate, PermPasswordSet,
		PermInstitutionRead, PermInstitutionWrite, PermInstitutionDelete,
		PermPlatformSettings, PermUserRoleWrite, PermAuditRead, PermImpersonate,
	},

	models.RoleSupport: {
		// Read-only, and deliberately NOT self-service: a support session must
		// not be able to change the operator's own account either, so the
		// profile write permissions are withheld. Everything below is a read.
		PermSupportRead,
		PermFacultyRead, PermCourseRead, PermModuleRead, PermClassRead,
		PermRoomRead, PermSubjectRead, PermStaffRead, PermTimetableRead,
		PermExamRead,
	},
}

// HasPermission reports whether a role holds a permission.
//
// An unknown role holds nothing. That is deliberate: a typo'd or
// not-yet-implemented role must fail closed rather than inherit admin rights.
func HasPermission(role models.UserRole, permission Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == permission {
			return true
		}
	}
	return false
}

// HasAnyPermission reports whether a role holds at least one of the
// permissions. Used where a route accepts several alternative capabilities.
func HasAnyPermission(role models.UserRole, permissions ...Permission) bool {
	for _, p := range permissions {
		if HasPermission(role, p) {
			return true
		}
	}
	return false
}

// HasAllPermissions reports whether a role holds every listed permission.
func HasAllPermissions(role models.UserRole, permissions ...Permission) bool {
	for _, p := range permissions {
		if !HasPermission(role, p) {
			return false
		}
	}
	return true
}

// PermissionsFor returns the sorted permission list for a role, for the
// /me/permissions endpoint and for the frontend's nav gating.
func PermissionsFor(role models.UserRole) []Permission {
	perms := rolePermissions[role]
	out := make([]Permission, len(perms))
	copy(out, perms)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CanAdminister reports whether a role may create, modify, or delete users
// within its own institution.
//
// An institution admin may administer their own institution's users, including
// other admins — but never super_admins, which is enforced separately in the
// user controller since it depends on the caller's institution, not just role.
func CanAdminister(role models.UserRole) bool {
	return HasPermission(role, PermUserWrite)
}

// writePermissions is the set of permissions that change shared state. It is
// used to answer "can this role change the institution's data?", which the
// frontend needs for a single global banner and the impersonation response
// needs for its guarantee.
//
// Self-service (updating one's own profile and password) is deliberately NOT in
// this set. Every real account has it, so including it would make every
// authenticated human "not read-only" and the signal would be useless. What the
// question actually means is "can this session change anything anybody else can
// see?".
//
// Listing the writes explicitly (rather than inferring them from a naming
// convention) means a new write permission must be added here, and the
// IsReadOnly test fails until it is.
var writePermissions = []Permission{
	PermFacultyWrite, PermCourseWrite, PermModuleWrite, PermClassWrite,
	PermRoomWrite, PermSubjectWrite, PermStaffWrite,
	PermTimetableGenerate, PermTimetablePublish, PermTimetableApprove, PermTimetableOverride,
	PermExamWrite, PermExamSchedule, PermExamPublish, PermExamApprove,
	PermUserWrite, PermUserDelete, PermStaffLink, PermDataImport, PermSettingsWrite,
	PermInstitutionWrite, PermInstitutionDelete, PermPlatformSettings,
	PermUserRoleWrite, PermImpersonate,
}

// IsWrite reports whether a permission changes shared state. Self-service
// permissions are not writes by this definition.
func IsWrite(permission Permission) bool {
	for _, p := range writePermissions {
		if p == permission {
			return true
		}
	}
	return false
}

// IsReadOnly reports whether a role can change nothing that anybody else can
// see.
//
// This is stricter than "lacks a particular write permission". An exam
// coordinator cannot edit the curriculum but does write exams, so it is not
// read-only; a lecturer can change their own password but no shared state, so
// they are; a support session writes nothing at all.
func IsReadOnly(role models.UserRole) bool {
	for _, p := range writePermissions {
		if HasPermission(role, p) {
			return false
		}
	}
	return true
}

// SupportPermissions returns the sorted permission list a support session
// holds. Exposed so the impersonation response can state the session's reach
// rather than leaving the operator to assume it.
func SupportPermissions() []string {
	perms := PermissionsFor(models.RoleSupport)
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, string(p))
	}
	return out
}

// AssignableRoles returns the roles a caller may grant.
//
//	platform super_admin → every institution role plus super_admin
//	everyone else        → only the plain `user` role
//
// The narrow answer for non-platform callers is the point: a tenant admin
// should not be able to mint a coordinator or another admin, because that would
// let a compromised tenant admin escalate. Promotion goes through the platform.
//
// The support role is never in the list for anyone, including a platform
// super_admin. It exists only inside a short-lived support token; persisting it
// on a user row would create a permanent read-only account and a second way to
// reach tenant data.
func AssignableRoles(callerRole models.UserRole) []models.UserRole {
	if callerRole == models.RoleSuperAdmin {
		out := make([]models.UserRole, 0, len(models.AllRoles))
		for _, role := range models.AllRoles {
			if role.IsSupportRole() {
				continue
			}
			out = append(out, role)
		}
		return out
	}
	return []models.UserRole{models.RoleUser}
}

// CanAssign reports whether a caller may grant targetRole. It does not consider
// the institution, which the user controller checks separately.
func CanAssign(callerRole, targetRole models.UserRole) bool {
	for _, allowed := range AssignableRoles(callerRole) {
		if allowed == targetRole {
			return true
		}
	}
	return false
}

// isKnownPermission reports whether a permission string is one the system
// defines. Validated at init so a typo in rolePermissions cannot silently deny
// access forever.
func isKnownPermission(p Permission) bool {
	for _, known := range allPermissions {
		if known == p {
			return true
		}
	}
	return false
}

// validate checks the permission map's internal consistency at startup:
//
//   - every role in models.AllRoles has an entry
//   - every permission granted is a permission the system defines
//   - no role outside models.AllRoles has an entry
//
// Called from an init in this package, so a mistake fails immediately at boot
// rather than at the first request that happens to need the permission.
func validate() {
	defined := map[models.UserRole]bool{}
	for _, r := range models.AllRoles {
		defined[r] = true
		if _, ok := rolePermissions[r]; !ok {
			panic("auth: role " + string(r) + " has no permission entry")
		}
	}
	for role, perms := range rolePermissions {
		if !defined[role] {
			panic("auth: permission entry for unknown role " + string(role))
		}
		for _, p := range perms {
			if !isKnownPermission(p) {
				panic("auth: role " + string(role) + " grants unknown permission " + string(p))
			}
		}
	}
}

func init() {
	validate()
}
