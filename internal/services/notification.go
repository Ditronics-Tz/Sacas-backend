package services

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"github.com/twilio/twilio-go"
	api "github.com/twilio/twilio-go/rest/api/v2010"

	"go_boilerplate/internal/config"
)

func GenerateOTP(length int) string {
	const charset = "0123456789"
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

type NotificationService struct {
	sendgridClient *sendgrid.Client
	twilioClient   *twilio.RestClient
	fromEmail      string
	fromPhone      string
}

func NewNotificationService() *NotificationService {
	sendgridAPIKey := config.GetEnv("SENDGRID_API_KEY", "")
	twilioAccountSid := config.GetEnv("TWILIO_ACCOUNT_SID", "")
	twilioAuthToken := config.GetEnv("TWILIO_AUTH_TOKEN", "")
	fromEmail := config.GetEnv("FROM_EMAIL", "noreply@example.com")
	fromPhone := config.GetEnv("FROM_PHONE", "+1234567890")

	sendgridClient := sendgrid.NewSendClient(sendgridAPIKey)
	twilioClient := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: twilioAccountSid,
		Password: twilioAuthToken,
	})

	return &NotificationService{
		sendgridClient: sendgridClient,
		twilioClient:   twilioClient,
		fromEmail:      fromEmail,
		fromPhone:      fromPhone,
	}
}

func (ns *NotificationService) SendEmailOTP(toEmail, otp string) error {
	from := mail.NewEmail("Go Boilerplate", ns.fromEmail)
	subject := "Your OTP Code"
	to := mail.NewEmail("Recipient", toEmail)
	plainTextContent := fmt.Sprintf("Your OTP code is %s. It is valid for 5 minutes.", otp)
	htmlContent := fmt.Sprintf("<strong>Your OTP code is %s. It is valid for 5 minutes.</strong>", otp)
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
	_, err := ns.sendgridClient.Send(message)
	return err
}

func (ns *NotificationService) SendSMSOTP(toPhone, otp string) error {
	params := &api.CreateMessageParams{}
	params.SetTo(toPhone)
	params.SetFrom(ns.fromPhone)
	params.SetBody(fmt.Sprintf("Your OTP code is %s. It is valid for 5 minutes.", otp))

	_, err := ns.twilioClient.Api.CreateMessage(params)
	return err
}

func (ns *NotificationService) SendTransactionalEmail(toEmail, subject, template string, data map[string]interface{}) error {
	from := mail.NewEmail("Go Boilerplate", ns.fromEmail)
	to := mail.NewEmail("Recipient", toEmail)
	plainTextContent := template // Fallback, could be enhanced with text template rendering
	htmlContent := template      // Should be rendered with HTML template
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
	_, err := ns.sendgridClient.Send(message)
	return err
}