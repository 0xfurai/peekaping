package providers

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/netguard"
	"peekaping/internal/smtpclient"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

// EmailConfig holds the required EmailConfig for email notifications
// Should match the JSON structure stored in the DB
// Example JSON: {"smtp_host":"smtp.example.com","smtp_port":587,"username":"user","password":"pass","from":"noreply@example.com"}
type EmailConfig struct {
	SMTPSecure    bool   `json:"smtp_secure" endpoint:"true"`
	SMTPHost      string `json:"smtp_host" validate:"required" endpoint:"true" target:"host"`
	SMTPPort      int    `json:"smtp_port" validate:"required" endpoint:"true"`
	SMTPUsername  string `json:"username" validate:"required"`
	SMTPPassword  string `json:"password" validate:"required" secret:"true"`
	SMTPFrom      string `json:"from" validate:"required"`
	SMTPTo        string `json:"to" validate:"required"`
	SMTPCC        string `json:"cc"`
	SMTPBCC       string `json:"bcc"`
	CustomSubject string `json:"custom_subject"`
	CustomBody    string `json:"custom_body"`
}

// defaultEmailSubject is the subject of a notification email that has no custom one
const defaultEmailSubject = "Peekaping Notification"

type EmailSender struct {
	logger *zap.SugaredLogger
	// guard refuses a server at an address the install blocks (BLOCK_PRIVATE_TARGETS, PP-158). A nil
	// guard blocks nothing.
	guard *netguard.Guard
}

// NewEmailSender creates an EmailSender
func NewEmailSender(logger *zap.SugaredLogger) *EmailSender {
	return NewGuardedEmailSender(logger, nil)
}

// NewGuardedEmailSender creates an EmailSender that connects to its SMTP server through the guard
func NewGuardedEmailSender(logger *zap.SugaredLogger, guard *netguard.Guard) *EmailSender {
	return &EmailSender{logger: logger, guard: guard}
}

func (e *EmailSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[EmailConfig](configJSON)
}

func (e *EmailSender) Validate(configJSON string) error {
	cfg, err := e.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*EmailConfig))
}

func (e *EmailSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	m *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := e.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	cfg := cfgAny.(*EmailConfig)

	engine := liquid.NewEngine()

	bindings := PrepareTemplateBindings(m, heartbeat, message)

	finalSubject := defaultEmailSubject
	if cfg.CustomSubject != "" {
		if rendered, err := engine.ParseAndRenderString(cfg.CustomSubject, bindings); err == nil {
			finalSubject = rendered
		}
	}

	finalBody := message
	if cfg.CustomBody != "" {
		if rendered, err := engine.ParseAndRenderString(cfg.CustomBody, bindings); err == nil {
			finalBody = rendered
		}
	}

	to := cfg.SMTPTo
	auth := smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPHost)
	msg := []byte(fmt.Sprintf("To: %s\r\nSubject: %s\r\nFrom: %s\r\n\r\n%s", to, finalSubject, cfg.SMTPFrom, finalBody))
	server := smtpclient.Server{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Auth: auth, TLS: smtpclient.Opportunistic}
	if e.guard != nil {
		server.Dialer = e.guard.Dialer(&net.Dialer{})
	}
	if err := smtpclient.Send(ctx, server, cfg.SMTPFrom, []string{to}, msg); err != nil {
		return err
	}
	e.logger.Infof("Email notification sent successfully")
	return nil
}
