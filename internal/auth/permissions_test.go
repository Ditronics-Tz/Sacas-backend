package auth

import (
	"testing"

	"go_boilerplate/internal/models"
)

// TestEveryRoleHasPermissions guards the invariant the init-time validation
// also enforces: a role with no entry holds nothing, which would otherwise show
// up as a confusing 403 rather than a boot failure.
func TestEveryRoleHasPermissions(t *testing.T) {
	for _, role := range models.AllRoles {
		if len(rolePermissions[role]) == 0 {
			t.Errorf("role %s has no permissions", role)
		}
	}
}

// TestUnknownRoleHoldsNothing is the fail-closed property: a role that is not
// in the map must not inherit anything, so a typo cannot become an escalation.
func TestUnknownRoleHoldsNothing(t *testing.T) {
	unknown := models.UserRole("hacker")
	if HasPermission(unknown, PermFacultyWrite) {
		t.Error("an unknown role must hold no permissions")
	}
	if HasPermission(unknown, PermImpersonate) {
		t.Error("an unknown role must hold no platform permissions")
	}
	if len(PermissionsFor(unknown)) != 0 {
		t.Error("an unknown role must report an empty permission list")
	}
}

// TestSuperAdminHasOnlyPlatformPermissions is the policy the tenancy work rests
// on: a platform account is not bound to an institution, so it must not hold
// any permission that would let it read or write tenant data without a tenant
// scope. Support access exists instead, and is read-only.
func TestSuperAdminHasOnlyPlatformPermissions(t *testing.T) {
	platformOnly := map[Permission]bool{
		PermInstitutionRead:   true,
		PermInstitutionWrite:  true,
		PermInstitutionDelete: true,
		PermPlatformSettings:  true,
		PermUserRoleWrite:     true,
		PermAuditRead:         true,
		PermImpersonate:       true,
	}
	for perm := range platformOnly {
		if !HasPermission(models.RoleSuperAdmin, perm) {
			t.Errorf("super_admin should hold %s", perm)
		}
	}

	// Anything that would touch another institution's data must be absent.
	workspace := []Permission{
		PermFacultyRead, PermFacultyWrite, PermCourseRead, PermCourseWrite,
		PermModuleRead, PermModuleWrite, PermClassRead, PermClassWrite,
		PermRoomRead, PermRoomWrite, PermSubjectRead, PermSubjectWrite,
		PermStaffRead, PermStaffWrite, PermTimetableRead, PermTimetableGenerate,
		PermUserRead, PermUserWrite, PermUserDelete, PermAdminStats,
		PermSettingsWrite, PermDataImport,
	}
	for _, perm := range workspace {
		if HasPermission(models.RoleSuperAdmin, perm) {
			t.Errorf("super_admin must NOT hold %s", perm)
		}
	}
}

// TestSupportRoleIsReadOnly verifies a support session cannot change anything.
func TestSupportRoleIsReadOnly(t *testing.T) {
	reads := []Permission{
		PermSupportRead, PermFacultyRead, PermCourseRead, PermModuleRead,
		PermClassRead, PermRoomRead, PermSubjectRead, PermStaffRead,
		PermTimetableRead, PermExamRead,
	}
	for _, perm := range reads {
		if !HasPermission(models.RoleSupport, perm) {
			t.Errorf("support should hold read permission %s", perm)
		}
	}

	writes := []Permission{
		PermFacultyWrite, PermCourseWrite, PermModuleWrite, PermClassWrite,
		PermRoomWrite, PermSubjectWrite, PermStaffWrite,
		PermTimetableGenerate, PermTimetablePublish, PermTimetableOverride,
		PermExamWrite, PermExamSchedule, PermExamPublish,
		PermUserRead, PermUserWrite, PermUserDelete, PermAdminStats,
		PermDataImport, PermSettingsWrite, PermImpersonate,
		PermInstitutionRead, PermInstitutionWrite, PermInstitutionDelete,
		PermPlatformSettings, PermUserRoleWrite, PermAuditRead,
		// Self-service is withheld too: a support session must not be able to
		// change the operator's own account.
		PermProfileUpdate, PermPasswordSet,
	}
	for _, perm := range writes {
		if HasPermission(models.RoleSupport, perm) {
			t.Errorf("support must NOT hold %s", perm)
		}
	}
}

// TestCoordinatorDomainsAreSeparated asserts the two coordinators do not overlap
// into each other's domain, which is the point of splitting them out of
// `administrator`.
func TestCoordinatorDomainsAreSeparated(t *testing.T) {
	// The academic coordinator runs the curriculum and teaching timetable.
	if !HasPermission(models.RoleAcademicCoord, PermModuleWrite) {
		t.Error("academic_coordinator should manage modules")
	}
	if !HasPermission(models.RoleAcademicCoord, PermTimetableGenerate) {
		t.Error("academic_coordinator should generate timetables")
	}
	if HasPermission(models.RoleAcademicCoord, PermExamWrite) {
		t.Error("academic_coordinator should NOT write exams")
	}
	if HasPermission(models.RoleAcademicCoord, PermUserWrite) {
		t.Error("academic_coordinator should NOT administer users")
	}

	// The exam coordinator runs exams and may read the structure to schedule
	// them, but must not rewrite the curriculum.
	if !HasPermission(models.RoleExamCoord, PermExamSchedule) {
		t.Error("exam_coordinator should schedule exams")
	}
	if !HasPermission(models.RoleExamCoord, PermExamPublish) {
		t.Error("exam_coordinator should publish exams")
	}
	if HasPermission(models.RoleExamCoord, PermModuleWrite) {
		t.Error("exam_coordinator should NOT modify modules")
	}
	if HasPermission(models.RoleExamCoord, PermCourseWrite) {
		t.Error("exam_coordinator should NOT modify courses")
	}
	if HasPermission(models.RoleExamCoord, PermUserWrite) {
		t.Error("exam_coordinator should NOT administer users")
	}
}

// TestUserCannotWrite covers the baseline: a lecturer is read-only.
func TestUserCannotWrite(t *testing.T) {
	writes := []Permission{
		PermFacultyWrite, PermCourseWrite, PermModuleWrite, PermClassWrite,
		PermRoomWrite, PermSubjectWrite, PermStaffWrite,
		PermTimetableGenerate, PermTimetablePublish, PermTimetableOverride,
		PermExamWrite, PermUserWrite, PermUserDelete, PermSettingsWrite,
	}
	for _, perm := range writes {
		if HasPermission(models.RoleUser, perm) {
			t.Errorf("user must NOT hold %s", perm)
		}
	}
	// But a lecturer can read the structure they teach against.
	for _, perm := range []Permission{PermFacultyRead, PermCourseRead, PermTimetableRead} {
		if !HasPermission(models.RoleUser, perm) {
			t.Errorf("user should hold read permission %s", perm)
		}
	}
}

// TestIsReadOnly pins the meaning of the read_only signal the frontend and the
// impersonation response depend on: it means "cannot change anything anybody
// else can see", not "cannot do anything at all". A lecturer can change their
// own password and is still read-only with respect to the institution.
func TestIsReadOnly(t *testing.T) {
	readOnly := []models.UserRole{models.RoleUser, models.RoleSupport}
	for _, role := range readOnly {
		if !IsReadOnly(role) {
			t.Errorf("%s should be read-only", role)
		}
	}

	writable := []models.UserRole{
		models.RoleAdmin, models.RoleAcademicCoord, models.RoleExamCoord, models.RoleSuperAdmin,
	}
	for _, role := range writable {
		if IsReadOnly(role) {
			t.Errorf("%s should NOT be read-only", role)
		}
	}
}

// TestIsWriteExcludesSelfService guards the reason the read-only signal is
// useful at all: if changing one's own password counted as a write, every
// authenticated account would be "not read-only".
func TestIsWriteExcludesSelfService(t *testing.T) {
	for _, selfService := range []Permission{PermProfileUpdate, PermPasswordSet} {
		if IsWrite(selfService) {
			t.Errorf("%s is self-service and should not count as a shared-state write", selfService)
		}
	}
	for _, shared := range []Permission{
		PermFacultyWrite, PermUserWrite, PermTimetableGenerate, PermImpersonate,
	} {
		if !IsWrite(shared) {
			t.Errorf("%s changes shared state and should count as a write", shared)
		}
	}
}

// TestEveryWritePermissionIsHeldByAtLeastOneRole catches a permission that is
// defined and classified as a write but granted to nobody — usually a typo in
// the role map, and invisible otherwise.
func TestEveryWritePermissionIsHeldByAtLeastOneRole(t *testing.T) {
	for _, perm := range writePermissions {
		granted := false
		for _, role := range models.AllRoles {
			if HasPermission(role, perm) {
				granted = true
				break
			}
		}
		if !granted {
			t.Errorf("write permission %s is granted to no role", perm)
		}
	}
}

// TestAdminSupersetOfCoordinators asserts the institution admin is not weaker
// than the coordinators it manages, otherwise adding a coordinator would
// silently reduce the existing admin's reach.
func TestAdminSupersetOfCoordinators(t *testing.T) {
	for _, coordinator := range []models.UserRole{models.RoleAcademicCoord, models.RoleExamCoord, models.RoleUser} {
		for _, perm := range rolePermissions[coordinator] {
			if !HasPermission(models.RoleAdmin, perm) {
				t.Errorf("administrator should hold %s (held by %s)", perm, coordinator)
			}
		}
	}
}

// TestAssignableRoles is the anti-escalation rule: a tenant admin may only mint
// plain users, so a compromised tenant admin cannot create a peer admin or a
// coordinator.
func TestAssignableRoles(t *testing.T) {
	assignable := AssignableRoles(models.RoleAdmin)
	if len(assignable) != 1 || assignable[0] != models.RoleUser {
		t.Errorf("institution admin should only be able to assign `user`, got %v", assignable)
	}

	// A platform super_admin may assign every real role. The support role is
	// excluded here and checked separately: it must never be assignable.
	for _, role := range models.AllRoles {
		if role.IsSupportRole() {
			continue
		}
		if !CanAssign(models.RoleSuperAdmin, role) {
			t.Errorf("platform super_admin should be able to assign %s", role)
		}
	}

	// A tenant admin must not be able to promote anyone.
	for _, forbidden := range []models.UserRole{
		models.RoleAdmin, models.RoleSuperAdmin,
		models.RoleAcademicCoord, models.RoleExamCoord, models.RoleSupport,
	} {
		if CanAssign(models.RoleAdmin, forbidden) {
			t.Errorf("institution admin must NOT be able to assign %s", forbidden)
		}
	}
}

// TestSupportRoleIsNotAssignable guards the rule that the support role exists
// only inside a short-lived token and is never persisted on a user row.
func TestSupportRoleIsNotAssignable(t *testing.T) {
	for _, role := range models.AllRoles {
		if CanAssign(role, models.RoleSupport) {
			t.Errorf("role %s must not be able to assign the support role", role)
		}
	}
}

// TestPermissionsAreSorted guards the /me/permissions contract, which the
// frontend may cache.
func TestPermissionsAreSorted(t *testing.T) {
	for _, role := range models.AllRoles {
		perms := PermissionsFor(role)
		for i := 1; i < len(perms); i++ {
			if perms[i-1] > perms[i] {
				t.Errorf("permissions for %s are not sorted: %v", role, perms)
				break
			}
		}
	}
}

// TestPermissionsForDoesNotAliasTheMap guards against a caller mutating the
// package's own slice through the returned value.
func TestPermissionsForDoesNotAliasTheMap(t *testing.T) {
	first := PermissionsFor(models.RoleUser)
	if len(first) == 0 {
		t.Fatal("expected some permissions")
	}
	first[0] = "tampered"

	second := PermissionsFor(models.RoleUser)
	if second[0] == "tampered" {
		t.Error("PermissionsFor returned an alias of the internal slice")
	}
}

func TestHasAllPermissions(t *testing.T) {
	if !HasAllPermissions(models.RoleUser, PermProfileRead, PermTimetableRead) {
		t.Error("user should hold both")
	}
	if HasAllPermissions(models.RoleUser, PermProfileRead, PermUserWrite) {
		t.Error("user should not hold user:write, so HasAll must be false")
	}
}

func TestHasAnyPermission(t *testing.T) {
	if !HasAnyPermission(models.RoleUser, PermUserWrite, PermTimetableRead) {
		t.Error("user holds timetable:read, so HasAny must be true")
	}
	if HasAnyPermission(models.RoleUser, PermUserWrite, PermFacultyWrite) {
		t.Error("user holds neither, so HasAny must be false")
	}
}

func TestCanAdminister(t *testing.T) {
	if !CanAdminister(models.RoleAdmin) {
		t.Error("institution admin should be able to administer users")
	}
	for _, role := range []models.UserRole{
		models.RoleAcademicCoord, models.RoleExamCoord, models.RoleUser, models.RoleSupport,
	} {
		if CanAdminister(role) {
			t.Errorf("%s should not be able to administer users", role)
		}
	}
}

func TestSupportPermissionsStringsMatchRole(t *testing.T) {
	strs := SupportPermissions()
	perms := PermissionsFor(models.RoleSupport)
	if len(strs) != len(perms) {
		t.Fatalf("SupportPermissions returned %d entries, role has %d", len(strs), len(perms))
	}
}
