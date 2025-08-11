package providers

import (
	"context"
	"fmt"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"strings"

	liquid "github.com/osteele/liquid"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"go.uber.org/zap"
)

// SendGridConfig holds the required configuration for SendGrid notifications
// Should match the JSON structure stored in the DB
type SendGridConfig struct {
	APIKey    string `json:"api_key" validate:"required"`
	FromEmail string `json:"from_email" validate:"required,email"`
	ToEmail   string `json:"to_email" validate:"required"`
	CCEmail   string `json:"cc_email"`
	BCCEmail  string `json:"bcc_email"`
	Subject   string `json:"subject"`
}

type SendGridSender struct {
	logger *zap.SugaredLogger
}

// NewSendGridSender creates a SendGridSender
func NewSendGridSender(logger *zap.SugaredLogger) *SendGridSender {
	return &SendGridSender{logger: logger}
}

func (s *SendGridSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[SendGridConfig](configJSON)
}

func (s *SendGridSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*SendGridConfig))
}

func (s *SendGridSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	m *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	cfg := cfgAny.(*SendGridConfig)

	engine := liquid.NewEngine()
	bindings := PrepareTemplateBindings(m, heartbeat, message)

	// Prepare subject with template support
	finalSubject := "Peekaping Notification"
	if cfg.Subject != "" {
		if rendered, err := engine.ParseAndRenderString(cfg.Subject, bindings); err == nil {
			finalSubject = rendered
		}
	}

	// Create SendGrid message
	sgMessage := mail.NewSingleEmail(
		mail.NewEmail("", cfg.FromEmail),
		finalSubject,
		mail.NewEmail("", cfg.ToEmail),
		message,
		"", // No HTML content for now, just plain text
	)

	// Add CC recipients if provided
	if cfg.CCEmail != "" {
		ccEmails := strings.Split(cfg.CCEmail, ",")
		for _, email := range ccEmails {
			email = strings.TrimSpace(email)
			if email != "" {
				sgMessage.Personalizations[0].AddCCs(mail.NewEmail("", email))
			}
		}
	}

	// Add BCC recipients if provided
	if cfg.BCCEmail != "" {
		bccEmails := strings.Split(cfg.BCCEmail, ",")
		for _, email := range bccEmails {
			email = strings.TrimSpace(email)
			if email != "" {
				sgMessage.Personalizations[0].AddBCCs(mail.NewEmail("", email))
			}
		}
	}

	// Create SendGrid client and send
	client := sendgrid.NewSendClient(cfg.APIKey)
	response, err := client.Send(sgMessage)
	
	if err != nil {
		s.logger.Errorw("Failed to send SendGrid email", "error", err)
		return fmt.Errorf("failed to send SendGrid email: %w", err)
	}

	// Check response status
	if response.StatusCode >= 400 {
		s.logger.Errorw("SendGrid returned error status", 
			"status", response.StatusCode,
			"body", response.Body,
			"headers", response.Headers)
		return fmt.Errorf("SendGrid API error: %d - %s", response.StatusCode, response.Body)
	}

	s.logger.Infow("SendGrid email sent successfully",
		"to", cfg.ToEmail,
		"from", cfg.FromEmail,
		"subject", finalSubject,
		"status", response.StatusCode)

	return nil
}