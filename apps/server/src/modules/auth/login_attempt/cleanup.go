package login_attempt

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type CleanupService struct {
	bruteforceService BruteforceService
	logger            *zap.SugaredLogger
	stopChan          chan struct{}
	doneChan          chan struct{}
}

func NewCleanupService(
	bruteforceService BruteforceService,
	logger *zap.SugaredLogger,
) *CleanupService {
	return &CleanupService{
		bruteforceService: bruteforceService,
		logger:            logger.Named("[login-attempt-cleanup]"),
		stopChan:          make(chan struct{}),
		doneChan:          make(chan struct{}),
	}
}

// StartCleanupJob starts a background goroutine that periodically cleans up old login attempts
func (c *CleanupService) StartCleanupJob(ctx context.Context, interval time.Duration) {
	c.logger.Infow("Starting login attempt cleanup job", "interval", interval)

	go func() {
		defer close(c.doneChan)
		
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run cleanup immediately on start
		c.performCleanup(ctx)

		for {
			select {
			case <-ctx.Done():
				c.logger.Infow("Context cancelled, stopping cleanup job")
				return
			case <-c.stopChan:
				c.logger.Infow("Stop signal received, stopping cleanup job")
				return
			case <-ticker.C:
				c.performCleanup(ctx)
			}
		}
	}()
}

// StopCleanupJob stops the background cleanup job
func (c *CleanupService) StopCleanupJob() {
	c.logger.Infow("Stopping login attempt cleanup job")
	close(c.stopChan)
	<-c.doneChan
	c.logger.Infow("Login attempt cleanup job stopped")
}

// performCleanup executes the actual cleanup operation
func (c *CleanupService) performCleanup(ctx context.Context) {
	c.logger.Debugw("Performing login attempt cleanup")
	
	start := time.Now()
	err := c.bruteforceService.CleanupOldAttempts(ctx)
	duration := time.Since(start)
	
	if err != nil {
		c.logger.Errorw("Failed to cleanup old login attempts", "error", err, "duration", duration)
	} else {
		c.logger.Infow("Successfully completed login attempt cleanup", "duration", duration)
	}
}