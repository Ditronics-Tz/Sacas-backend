package models

import "time"

// AuditAction is the kind of thing that was done. Kept as a string type rather
// than an enum table because new actions get added as features ship, and the
// action column is indexed for filtering rather than constrained.
type AuditAction string

const (
	// Identity and access — the highest-value rows to keep.
	AuditRoleChange      AuditAction = "role.change"
	AuditUserCreate      AuditAction = "user.create"
	AuditUserUpdate      AuditAction = "user.update"
	AuditUserDelete      AuditAction = "user.delete"
	AuditUserSuspend     AuditAction = "user.suspend"
	AuditUserActivate    AuditAction = "user.activate"
	AuditPasswordReset   AuditAction = "password.reset"
	AuditRoleSelfElevate AuditAction = "role.self_elevate"

	// Institution lifecycle.
	AuditInstitutionCreate     AuditAction = "institution.create"
	AuditInstitutionUpdate     AuditAction = "institution.update"
	AuditInstitutionSuspend    AuditAction = "institution.suspend"
	AuditInstitutionDelete     AuditAction = "institution.delete"
	AuditInstitutionPlanChange AuditAction = "institution.plan_change"

	// Support access. Impersonation is the most sensitive row in the table.
	AuditImpersonateStart AuditAction = "impersonate.start"
	AuditImpersonateEnd   AuditAction = "impersonate.end"

	// Academic data.
	AuditTimetableGenerate AuditAction = "timetable.generate"
	AuditTimetablePublish  AuditAction = "timetable.publish"
	AuditTimetableApprove  AuditAction = "timetable.approve"
	AuditExamPublish       AuditAction = "exam.publish"
	AuditExamApprove       AuditAction = "exam.approve"
	AuditDataImport        AuditAction = "data.import"
)

// AuditOutcome records whether the action succeeded. A denied attempt is
// recorded too: a stream of failures on a sensitive action is exactly the
// signal a support engineer looks for.
type AuditOutcome string

const (
	AuditOutcomeSuccess AuditOutcome = "success"
	AuditOutcomeDenied  AuditOutcome = "denied"
	AuditOutcomeFailure AuditOutcome = "failure"
)

// AuditLog is an append-only record of a consequential action.
//
// "Append-only" is enforced by convention and by the absence of any update or
// delete path in the repository: rows are written and read, never edited or
// removed. There is deliberately no soft-delete column, because a deletable
// audit trail is not an audit trail.
//
// The acting user is recorded alongside the institution so that a platform
// action (no institution) and a tenant action (with one) can be told apart, and
// so an institution's own trail can be read without scanning every row.
type AuditLog struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// RecordedAt is the action time. CreatedAt is deliberately not used: an
	// audit row must carry when the event happened, not when it was written
	// (which can differ under queueing or retry).
	RecordedAt time.Time `gorm:"not null;index" json:"recorded_at"`

	// ActorID is the user who performed the action. A 0 value means the action
	// was performed by the system (a migration or a seed), not by a person.
	ActorID    uint   `gorm:"index" json:"actor_id"`
	ActorEmail string `json:"actor_email"`
	ActorRole  string `json:"actor_role"`

	// InstitutionID is the tenant the action belongs to. Null for platform
	// actions, which is what distinguishes them from tenant actions.
	InstitutionID *uint `gorm:"index" json:"institution_id,omitempty"`

	Action  AuditAction  `gorm:"not null;index" json:"action"`
	Outcome AuditOutcome `gorm:"not null" json:"outcome"`
	Target  string       `json:"target"`
	// TargetID and TargetType are separated so the trail can be filtered by
	// object without string matching on a formatted value.
	TargetID   string `json:"target_id,omitempty"`
	TargetType string `json:"target_type,omitempty"`

	// Detail carries structured context (JSON-encoded by the caller). Never
	// secrets: the writer is responsible for redacting.
	Detail string `gorm:"type:text" json:"detail,omitempty"`

	// IPAddress and UserAgent support abuse investigation. Recorded from the
	// request, which the client can spoof behind a proxy, so treat it as a
	// hint rather than proof.
	IPAddress string `gorm:"type:inet" json:"ip_address,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

// Record builds an audit row for a successful action by the given actor.
func Record(actor *User, institutionID *uint, action AuditAction, target string, detail string) *AuditLog {
	entry := &AuditLog{
		RecordedAt:    time.Now().UTC(),
		InstitutionID: institutionID,
		Action:        action,
		Outcome:       AuditOutcomeSuccess,
		Target:        target,
		Detail:        detail,
	}
	if actor != nil {
		entry.ActorID = actor.ID
		entry.ActorEmail = actor.Email
		entry.ActorRole = string(actor.Role)
	}
	return entry
}

// RecordDenied builds an audit row for a refused action. Recorded as its own
// outcome so a scan for denied impersonation or denied role escalation is a
// simple filtered query.
func RecordDenied(actor *User, institutionID *uint, action AuditAction, target string, reason string) *AuditLog {
	entry := Record(actor, institutionID, action, target, reason)
	entry.Outcome = AuditOutcomeDenied
	return entry
}

// WithTargetType sets the object type for a filtered trail.
func (a *AuditLog) WithTargetType(t string) *AuditLog {
	a.TargetType = t
	return a
}

// WithTargetID sets the object ID for a filtered trail.
func (a *AuditLog) WithTargetID(id string) *AuditLog {
	a.TargetID = id
	return a
}

// WithRequest attaches request metadata. Called by the audit helper in the
// controllers so no handler has to remember it.
func (a *AuditLog) WithRequest(ip, userAgent string) *AuditLog {
	a.IPAddress = ip
	a.UserAgent = userAgent
	return a
}
