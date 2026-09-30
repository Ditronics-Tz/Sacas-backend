package repositories

import (
	"errors"

	"gorm.io/gorm"
)

// ErrCrossTenant is returned when a record exists but belongs to a different
// institution. Callers surface this as 404, not 403, so the API does not leak
// the existence of another tenant's records.
var ErrCrossTenant = errors.New("record not found in this institution")

// PlatformScope is the institution ID that means "no tenant filter", reserved
// for platform super_admins. Real institutions start at 1.
const PlatformScope uint = 0

// IsPlatformScope reports whether an institution ID means "all institutions".
func IsPlatformScope(institutionID uint) bool { return institutionID == PlatformScope }

// TenantScope applies the institution filter to a query.
//
// A zero institutionID means the caller is a platform super_admin and the
// query is left unscoped. Any other value constrains the query to that
// institution, so a record owned by another tenant is indistinguishable from
// one that does not exist.
//
// Every repository method that reads or writes tenant data must pass through
// here. Scoping the query — rather than filtering after the fact — is what
// makes a cross-tenant get/update/delete return ErrRecordNotFound.
func TenantScope(db *gorm.DB, institutionID uint) *gorm.DB {
	if db == nil || IsPlatformScope(institutionID) {
		return db
	}
	return db.Where("institution_id = ?", institutionID)
}

// InTenant reports whether a record with the given institution ID is visible
// to a caller scoped to institutionID.
func InTenant(recordInstitutionID, institutionID uint) bool {
	return IsPlatformScope(institutionID) || recordInstitutionID == institutionID
}

// SetTenantColumn stamps the institution on a record that GORM is about to
// write. A zero institutionID would silently create an orphan row, so callers
// that create tenant data should either set the field themselves or go
// through here.
func SetTenantColumn(db *gorm.DB, institutionID uint, model any) *gorm.DB {
	if db == nil || IsPlatformScope(institutionID) {
		return db
	}
	return db.Model(model).Update("institution_id", institutionID)
}
