package providers

import (
	"context"
	"fmt"
	"net/smtp"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

// EmailConfig holds the required EmailConfig for email notifications
// Should match the JSON structure stored in the DB
// Example JSON: {"smtp_host":"smtp.example.com","smtp_port":587,"username":"user","password":"pass","from":"noreply@example.com"}
type EmailConfig struct {
	SMTPSecure    bool   `json:"smtp_secure"`
	SMTPHost      string `json:"smtp_host" validate:"required"`
	SMTPPort      int    `json:"smtp_port" validate:"required"`
	SMTPUsername  string `json:"username" validate:"required"`
	SMTPPassword  string `json:"password" validate:"required"`
	SMTPFrom      string `json:"from" validate:"required"`
	SMTPTo        string `json:"to" validate:"required"`
	SMTPCC        string `json:"cc"`
	SMTPBCC       string `json:"bcc"`
	CustomSubject string `json:"custom_subject"`
	CustomBody    string `json:"custom_body"`
}

type EmailSender struct {
	logger *zap.SugaredLogger
}

// NewEmailSender creates an EmailSender
func NewEmailSender(logger *zap.SugaredLogger) *EmailSender {
	return &EmailSender{logger: logger}
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

	finalSubject := "Vigi Notification"
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
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	fmt.Println("Sending email to:", to, "from:", cfg.SMTPFrom, "subject:", finalSubject, "body:", finalBody)
	return smtp.SendMail(addr, auth, cfg.SMTPFrom, []string{to}, msg)
}
