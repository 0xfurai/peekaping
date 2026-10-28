package shutdown

import (
	"time"

	"peekaping/internal/mailer"

	"go.uber.org/zap"
)

// MailerDrain is how long a shutdown waits for the account emails still being sent
const MailerDrain = 10 * time.Second

// DrainMailer lets the account emails that are being sent go out, for up to timeout, and logs how
// many it gave up on. Call it once nothing can start a send any more (the HTTP handlers have
// returned), and before the database closes: a send reads and writes the database. It takes no
// time when nothing is being sent, and when the install has no sender.
func DrainMailer(m *mailer.Mailer, timeout time.Duration, logger *zap.SugaredLogger) {
	if dropped := m.WaitFor(timeout); dropped > 0 {
		logger.Errorw("Gave up waiting for account emails that were still being sent", "dropped", dropped, "waited", timeout.String())
	}
}
