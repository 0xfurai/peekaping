package providers

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"strconv"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

// EmailConfig holds the required EmailConfig for email notifications
// Should match the JSON structure stored in the DB
// Example JSON: {"smtp_host":"smtp.example.com","smtp_port":587,"username":"user","password":"pass","from":"noreply@example.com"}
type EmailConfig struct {
	SMTPSecure    bool   `json:"smtp_secure" endpoint:"true"`
	SMTPHost      string `json:"smtp_host" validate:"required" endpoint:"true"`
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

	finalSubject := "Peekaping Notification"
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
	addr := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))
	fmt.Println("Sending email to:", to, "from:", cfg.SMTPFrom, "subject:", finalSubject, "body:", finalBody)
	return sendMail(ctx, addr, auth, cfg.SMTPFrom, []string{to}, msg)
}

// sendMail does what smtp.SendMail does, but stops when ctx is done. smtp.SendMail has no
// context or timeout, so a server that never answers would hold the send forever.
func sendMail(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	// Closing the connection unblocks the SMTP client once ctx is done
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	err = sendMailOn(conn, host, auth, from, to, msg)
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("failed to send email: %w", ctx.Err())
	}
	return err
}

func sendMailOn(conn net.Conn, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer c.Close()

	if err := c.Hello("localhost"); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
