package services

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html"
	"math/big"
	"net/http"
	"strings"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"github.com/twilio/twilio-go"
	api "github.com/twilio/twilio-go/rest/api/v2010"

	"go_boilerplate/internal/config"
	"go_boilerplate/pkg/logger"
)

// ---------------------------------------------------------------------------
// OTP — cryptographically secure
// ---------------------------------------------------------------------------

func GenerateOTP(length int) string {
	const charset = "0123456789"
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			// crypto/rand failure is fatal — should never happen on a healthy OS
			panic(fmt.Sprintf("crypto/rand failed: %v", err))
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Provider interfaces — email and SMS behind abstractions so we can swap
// SendGrid/Twilio for Tanzania-local providers or a log-only dev provider.
// ---------------------------------------------------------------------------

// EmailProvider sends a single email.
type EmailProvider interface {
	Send(toEmail, subject, plainText, htmlContent string) error
}

// SMSProvider sends a single SMS.
type SMSProvider interface {
	Send(toPhone, body string) error
}

// --- SendGrid implementation ---

type sendGridEmailProvider struct {
	client    *sendgrid.Client
	fromEmail string
	fromName  string
}

func (p *sendGridEmailProvider) Send(toEmail, subject, plainText, htmlContent string) error {
	from := mail.NewEmail(p.fromName, p.fromEmail)
	to := mail.NewEmail("Recipient", toEmail)
	msg := mail.NewSingleEmail(from, subject, to, plainText, htmlContent)
	_, err := p.client.Send(msg)
	return err
}

// --- Twilio implementation ---

type twilioSMSProvider struct {
	client    *twilio.RestClient
	fromPhone string
}

func (p *twilioSMSProvider) Send(toPhone, body string) error {
	params := &api.CreateMessageParams{}
	params.SetTo(toPhone)
	params.SetFrom(p.fromPhone)
	params.SetBody(body)
	_, err := p.client.Api.CreateMessage(params)
	return err
}

// --- Tanzania local SMS provider (Beem Africa compatible) ---
// Set BEEM_API_KEY + BEEM_SECRET_KEY + BEEM_SOURCE_ADDR to enable.
// API docs: https://beem.africa — generic POST; stub logs on failure.

type tanzaniaSMSProvider struct {
	apiKey     string
	secretKey  string
	sourceAddr string
	baseURL    string
	httpClient *http.Client
}

func (p *tanzaniaSMSProvider) Send(toPhone, body string) error {
	// Beem expects E.164 without leading '+'; we normalise
	payload := map[string]interface{}{
		"source_addr":   p.sourceAddr,
		"schedule_time": "",
		"encoding":      0,
		"message":       body,
		"recipients": []map[string]string{
			{"recipient_id": "1", "dest_addr": strings.TrimPrefix(toPhone, "+")},
		},
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, p.baseURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(p.apiKey, p.secretKey)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("tanzania SMS provider returned %d", resp.StatusCode)
	}
	return nil
}

// --- Log providers (development) ---

type logEmailProvider struct {
	fromEmail string
	fromName  string
}

func (p *logEmailProvider) Send(toEmail, subject, plainText, _ string) error {
	logger.Info("[LOG EMAIL] from=%s <%s> to=%s subject=%q body=%q", p.fromName, p.fromEmail, toEmail, subject, plainText)
	return nil
}

type logSMSProvider struct {
	fromPhone string
}

func (p *logSMSProvider) Send(toPhone, body string) error {
	logger.Info("[LOG SMS] from=%s to=%s body=%q", p.fromPhone, toPhone, body)
	return nil
}

// ---------------------------------------------------------------------------
// NotificationService — composes an EmailProvider + SMSProvider
// ---------------------------------------------------------------------------

type NotificationService struct {
	email EmailProvider
	sms   SMSProvider

	// retained for branding / tests
	fromEmail string
	fromPhone string
	fromName  string
}

// NewNotificationService builds providers from env. In production it returns
// an error if required provider keys are missing so the process fails fast
// (see ValidateProductionNotificationConfig).
func NewNotificationService() (*NotificationService, error) {
	env := strings.ToLower(config.GetEnv("ENV", "development"))
	isProd := env == "production" || env == "prod"

	fromName := config.GetEnv("EMAIL_FROM_NAME", "SACAS")
	fromEmail := config.GetEnv("FROM_EMAIL", "")
	fromPhone := config.GetEnv("FROM_PHONE", "")

	// Defaults: development log providers are fine; production must supply
	// real values.
	if fromEmail == "" {
		if isProd {
			return nil, fmt.Errorf("FROM_EMAIL is required when ENV=production (e.g. noreply@sacas.ac.tz)")
		}
		fromEmail = "noreply@sacas.local"
	}
	if strings.EqualFold(fromEmail, "noreply@example.com") && isProd {
		return nil, fmt.Errorf("FROM_EMAIL must not be noreply@example.com in production")
	}
	if fromPhone == "" && !isProd {
		fromPhone = "+255000000000"
	}

	// --- Email provider ---
	var email EmailProvider
	if key := config.GetEnv("SENDGRID_API_KEY", ""); key != "" {
		if fromEmail == "" {
			return nil, fmt.Errorf("SENDGRID_API_KEY set but FROM_EMAIL is empty")
		}
		email = &sendGridEmailProvider{
			client:    sendgrid.NewSendClient(key),
			fromEmail: fromEmail,
			fromName:  fromName,
		}
	} else {
		if isProd {
			return nil, fmt.Errorf("SENDGRID_API_KEY is required when ENV=production (no email provider configured)")
		}
		email = &logEmailProvider{fromEmail: fromEmail, fromName: fromName}
	}

	// --- SMS provider ---
	var sms SMSProvider
	twSid := config.GetEnv("TWILIO_ACCOUNT_SID", "")
	twToken := config.GetEnv("TWILIO_AUTH_TOKEN", "")
	beemKey := config.GetEnv("BEEM_API_KEY", "")
	beemSecret := config.GetEnv("BEEM_SECRET_KEY", "")
	beemSource := config.GetEnv("BEEM_SOURCE_ADDR", fromPhone)
	beemURL := config.GetEnv("BEEM_API_URL", "https://apisms.beem.africa/v1/send")

	switch {
	case twSid != "" && twToken != "":
		if fromPhone == "" {
			return nil, fmt.Errorf("TWILIO_ACCOUNT_SID set but FROM_PHONE is empty")
		}
		sms = &twilioSMSProvider{
			client: twilio.NewRestClientWithParams(twilio.ClientParams{
				Username: twSid,
				Password: twToken,
			}),
			fromPhone: fromPhone,
		}
	case beemKey != "" && beemSecret != "":
		if beemSource == "" {
			return nil, fmt.Errorf("BEEM_API_KEY set but BEEM_SOURCE_ADDR/FROM_PHONE is empty")
		}
		sms = &tanzaniaSMSProvider{
			apiKey:     beemKey,
			secretKey:  beemSecret,
			sourceAddr: beemSource,
			baseURL:    beemURL,
			httpClient: &http.Client{},
		}
	default:
		if isProd {
			// Production requires an SMS provider — OTP via SMS is a core flow.
			// If you intentionally disable SMS, set SMS_PROVIDER=none explicitly.
			if strings.EqualFold(config.GetEnv("SMS_PROVIDER", ""), "none") {
				sms = &logSMSProvider{fromPhone: fromPhone}
			} else {
				return nil, fmt.Errorf("no SMS provider configured when ENV=production (set TWILIO_ACCOUNT_SID/TWILIO_AUTH_TOKEN or BEEM_API_KEY/BEEM_SECRET_KEY, or SMS_PROVIDER=none to explicitly disable)")
			}
		} else {
			sms = &logSMSProvider{fromPhone: fromPhone}
		}
	}

	return &NotificationService{
		email:     email,
		sms:       sms,
		fromEmail: fromEmail,
		fromPhone: fromPhone,
		fromName:  fromName,
	}, nil
}

// MustNewNotificationService is a convenience for call sites that have already
// validated production config and want to panic on error (kept for compat).
func MustNewNotificationService() *NotificationService {
	ns, err := NewNotificationService()
	if err != nil {
		panic(err)
	}
	return ns
}

func (ns *NotificationService) SendEmailOTP(toEmail, otp string) error {
	subject := "Your SACAS verification code"
	plain := fmt.Sprintf("Your SACAS OTP code is %s. It is valid for 5 minutes. Do not share this code.", otp)
	html := fmt.Sprintf("<p>Your SACAS OTP code is <strong>%s</strong>. It is valid for 5 minutes. Do not share this code.</p>", otp)
	return ns.email.Send(toEmail, subject, plain, html)
}

func (ns *NotificationService) SendSMSOTP(toPhone, otp string) error {
	body := fmt.Sprintf("Your SACAS OTP code is %s. It is valid for 5 minutes. Do not share this code.", otp)
	return ns.sms.Send(toPhone, body)
}

func (ns *NotificationService) SendTransactionalEmail(toEmail, subject, template string, data map[string]interface{}) error {
	// template is rendered plain/html already by caller
	return ns.email.Send(toEmail, subject, template, template)
}

// GeneratePassword returns a cryptographically random password that satisfies
// the shared password policy.
//
// Used when the platform resets a password: the secret is generated server-side
// and emailed to the account holder, so an administrator never handles it.
//
// The alphabet deliberately excludes look-alike characters (no 0/O, 1/l/I) so a
// generated password can be read aloud or copied from an email without
// ambiguity, and the loop guarantees at least one of each required class
// because the policy demands it.
func GeneratePassword(length int) (string, error) {
	if length < 12 {
		length = 12
	}
	const (
		lower  = "abcdefghijkmnopqrstuvwxyz"
		upper  = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		digits = "23456789"
		all    = lower + upper + digits
	)

	out := make([]byte, 0, length)
	// One character from each required class, so the result always satisfies
	// the policy without a retry loop.
	for _, class := range []string{lower, upper, digits} {
		c, err := randomChar(class)
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		out = append(out, c)
	}
	for len(out) < length {
		c, err := randomChar(all)
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		out = append(out, c)
	}
	// Shuffle, so the guaranteed characters are not always in the first
	// positions for anyone who knows the algorithm.
	for i := len(out) - 1; i > 0; i-- {
		j, err := randomIndex(i + 1)
		if err != nil {
			return "", fmt.Errorf("shuffle password: %w", err)
		}
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}

// randomChar picks one byte uniformly from an alphabet.
func randomChar(alphabet string) (byte, error) {
	n, err := randomIndex(len(alphabet))
	if err != nil {
		return 0, err
	}
	return alphabet[n], nil
}

// randomIndex returns a uniform value in [0, n).
//
// crypto/rand.Int is used rather than a raw byte modulo n, because a byte mod n
// is biased whenever n does not divide 256. That bias is irrelevant for a
// display token but worth avoiding in a password.
func randomIndex(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("random index bound must be positive, got %d", n)
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

// SendPasswordReset emails a platform-issued password to the account holder.
//
// Separate from SendEmailOTP so the copy says plainly that this is a password
// and not a short verification code: a user who confuses the two will type a
// six-digit code as their password and lock themselves out.
func (ns *NotificationService) SendPasswordReset(toEmail, password string) error {
	subject := "Your SACAS account password has been reset"
	plain := strings.Join([]string{
		"Your SACAS account password was reset by a platform administrator.",
		"",
		"New password: " + password,
		"",
		"If you were not expecting this, contact your institution administrator immediately.",
	}, "\n")
	html := "<p>Your SACAS account password was reset by a platform administrator.</p>" +
		"<p><strong>New password:</strong> <code>" + html.EscapeString(password) + "</code></p>" +
		"<p>If you were not expecting this, contact your institution administrator immediately.</p>"
	return ns.email.Send(toEmail, subject, plain, html)
}
