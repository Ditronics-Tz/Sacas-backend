package controllers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"

	"go_boilerplate/internal/config"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
	"go_boilerplate/pkg/security"
)

// OnboardingController serves the public institution signup and the public
// invitation acceptance.
//
// Both endpoints create a database row from an unauthenticated request, so both
// are rate limited at the router and captcha-gated by the captcha service.
type OnboardingController struct {
	onboarding  *services.OnboardingService
	invitations *services.InvitationService
	invRepo     repositories.InvitationRepository
	captcha     *services.CaptchaService
	notify      *services.NotificationService
	redis       *redis.Client
	otpGuard    *services.OTPAttemptGuard
}

func NewOnboardingController(
	onboarding *services.OnboardingService,
	invitations *services.InvitationService,
	invRepo repositories.InvitationRepository,
	captcha *services.CaptchaService,
	notify *services.NotificationService,
	redisClient *redis.Client,
	otpGuard *services.OTPAttemptGuard,
) *OnboardingController {
	return &OnboardingController{
		onboarding:  onboarding,
		invitations: invitations,
		invRepo:     invRepo,
		captcha:     captcha,
		notify:      notify,
		redis:       redisClient,
		otpGuard:    otpGuard,
	}
}

type RegisterInstitutionRequest struct {
	InstitutionName string `json:"institution_name" binding:"required,min=2,max=150"`
	InstitutionType string `json:"institution_type" binding:"required"`
	Region          string `json:"region,omitempty"`
	Country         string `json:"country,omitempty"`
	Phone           string `json:"phone,omitempty"`
	Address         string `json:"address,omitempty"`

	AdminEmail     string `json:"admin_email" binding:"required,email"`
	AdminPassword  string `json:"admin_password" binding:"required,min=8,strongpassword"`
	AdminFirstName string `json:"admin_first_name" binding:"required,min=2,max=50"`
	AdminLastName  string `json:"admin_last_name" binding:"required,min=2,max=50"`
	AdminPhone     string `json:"admin_phone,omitempty"`

	// CaptchaToken is the client-side captcha response. Required only when
	// CAPTCHA_ENABLED is true.
	CaptchaToken string `json:"captcha_token,omitempty"`
}

// RegisterInstitution handles POST /api/auth/register-institution.
//
// Creates a PENDING institution plus its first administrator in one
// transaction, then sends the email verification OTP through the same
// verify-email flow as an ordinary registration, so the two flows cannot drift
// apart.
func (oc *OnboardingController) RegisterInstitution(ctx *gin.Context) {
	var req RegisterInstitutionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		if strings.Contains(err.Error(), "assword") {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
			return
		}
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !security.ValidPassword(req.AdminPassword) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
		return
	}

	// Abuse control before any database work.
	if !oc.verifyCaptcha(ctx, req.CaptchaToken) {
		return
	}

	result, err := oc.onboarding.RegisterInstitution(services.RegisterInstitutionRequest{
		InstitutionName: req.InstitutionName,
		InstitutionType: req.InstitutionType,
		Region:          req.Region,
		Country:         req.Country,
		Phone:           req.Phone,
		Address:         req.Address,
		AdminEmail:      req.AdminEmail,
		AdminPassword:   req.AdminPassword,
		AdminFirstName:  req.AdminFirstName,
		AdminLastName:   req.AdminLastName,
		AdminPhone:      req.AdminPhone,
	})
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInstitutionNameTaken):
			ctx.JSON(http.StatusConflict, gin.H{"error": "An institution with this name already exists"})
			return
		case errors.Is(err, services.ErrEmailInUse):
			ctx.JSON(http.StatusConflict, gin.H{"error": "An account with this email already exists"})
			return
		case errors.Is(err, services.ErrOnboardingNotAllowed):
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		default:
			logger.Error("Institution registration failed: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register institution"})
			return
		}
	}

	// The same verification OTP flow as an ordinary registration: the key and
	// the endpoint the client posts to are identical, so verify-email works
	// unchanged for both.
	oc.sendVerificationOTP(ctx, result.Admin.Email, "institution registration")

	logger.Info("Public institution registration: %s (ID %d, pending approval)", result.Institution.Name, result.Institution.ID)
	ctx.JSON(http.StatusCreated, gin.H{
		"message": "Institution registered. Check your email for a verification code.",
		"institution": gin.H{
			"id":     result.Institution.ID,
			"name":   result.Institution.Name,
			"slug":   result.Institution.Slug,
			"status": result.Institution.Status,
			"type":   result.Institution.Type,
		},
		"user_id": result.Admin.ID,
		"status":  "pending_approval",
		"next": "Verify your email, then wait for platform approval. You can sign in once the " +
			"institution is activated and your email is verified.",
	})
}

type AcceptInvitationRequest struct {
	Token     string `json:"token" binding:"required"`
	Password  string `json:"password" binding:"required,min=8,strongpassword"`
	FirstName string `json:"first_name" binding:"required,min=2,max=50"`
	LastName  string `json:"last_name" binding:"required,min=2,max=50"`
	Phone     string `json:"phone,omitempty"`

	CaptchaToken string `json:"captcha_token,omitempty"`
}

// AcceptInvitation handles POST /api/auth/accept-invitation.
//
// No email verification step: the invitation was delivered to the address, so
// holding the token already proves control of it. The account is created active
// and verified, bound to the inviting institution.
func (oc *OnboardingController) AcceptInvitation(ctx *gin.Context) {
	var req AcceptInvitationRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		if strings.Contains(err.Error(), "assword") {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
			return
		}
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !security.ValidPassword(req.Password) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
		return
	}

	if !oc.verifyCaptcha(ctx, req.CaptchaToken) {
		return
	}

	user, err := oc.onboarding.AcceptInvitation(
		oc.invRepo, req.Token, req.Password, req.FirstName, req.LastName, req.Phone)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvitationInvalid):
			// One message for not-found, expired, revoked, and already-used, so
			// the response cannot be used to probe which it was.
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "This invitation is no longer valid",
				"details": "It may have expired, been revoked, or already been used.",
			})
			return
		case errors.Is(err, services.ErrEmailInUse):
			ctx.JSON(http.StatusConflict, gin.H{
				"error":   "An account with this email already exists",
				"details": "Sign in instead, or use a different address.",
			})
			return
		default:
			logger.Error("Invitation acceptance failed: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to accept invitation"})
			return
		}
	}

	ctx.JSON(http.StatusCreated, gin.H{
		"message": "Invitation accepted. You can sign in now.",
		"user": gin.H{
			"id":             user.ID,
			"email":          user.Email,
			"first_name":     user.FirstName,
			"last_name":      user.LastName,
			"role":           user.Role,
			"institution_id": user.InstitutionID,
		},
	})
}

// sendVerificationOTP issues and delivers the email verification code, reusing
// the existing Redis-backed flow so verify-email is shared with ordinary
// registration.
//
// A delivery failure is logged, not returned: the account exists and the
// resend-verification endpoint exists precisely for this case, so failing the
// signup would leave an unreachable row.
func (oc *OnboardingController) sendVerificationOTP(ctx *gin.Context, email, reason string) {
	otp := services.GenerateOTP(6)
	if oc.redis == nil {
		logger.Warn("Redis unavailable — cannot store verification OTP for %s (%s)", email, reason)
		return
	}
	if err := oc.redis.Set(ctx, "verify:"+email, otp, 15*time.Minute).Err(); err != nil {
		logger.Error("Failed to store verification OTP for %s: %v", email, err)
		return
	}
	if devOTPLogging() {
		logger.Info("DEV verification OTP for %s (%s): %s", email, reason, otp)
	}
	if oc.notify != nil {
		if err := oc.notify.SendEmailOTP(email, otp); err != nil {
			logger.Error("Failed to send verification email to %s: %v", email, err)
		}
	}
}

// verifyCaptcha runs the abuse check and writes the failure response. Returns
// false when the request must not proceed.
func (oc *OnboardingController) verifyCaptcha(ctx *gin.Context, token string) bool {
	if oc.captcha == nil || !oc.captcha.Enabled() {
		return true
	}
	ok, err := oc.captcha.Verify(ctx.Request.Context(), token, ctx.ClientIP())
	if err != nil {
		if services.IsCaptchaRequired(err) {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "Captcha required",
				"details": "Solve the captcha and send its token as captcha_token.",
			})
			return false
		}
		// A provider failure is a server-side problem, not the caller's.
		logger.Error("Captcha verification error: %v", err)
		ctx.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Captcha verification is temporarily unavailable",
			"details": "Please try again shortly.",
		})
		return false
	}
	if !ok {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Captcha verification failed"})
		return false
	}
	return true
}

// PublicRegisterEnabled reports whether the legacy self-service registration
// endpoint is available.
//
// The decision: public /auth/register is DISABLED by default, because a user with
// no institution can never sign in. Since the tenancy work, User.institution_id
// is required for every non-platform role and TenantMiddleware refuses a request
// with no tenant scope, so a public register can only ever create an account
// that is permanently locked out. New accounts come from an invitation, or from
// institution registration.
//
// The flag exists so a deployment with a pre-tenant migration path, or one that
// provisions accounts out of band, can re-enable it deliberately.
func PublicRegisterEnabled() bool {
	return strings.EqualFold(config.GetEnv("PUBLIC_REGISTER_ENABLED", "false"), "true")
}

// RegisterDisabledMessage explains the change to a caller who reaches the
// disabled endpoint, and names the alternative.
const RegisterDisabledMessage = "Self-service registration is closed. " +
	"Accounts are created by invitation, or by registering your institution at POST /api/auth/register-institution."
