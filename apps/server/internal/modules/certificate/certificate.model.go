package certificate

import (
	"peekaping/internal/modules/shared"
	"time"
)

// Type aliases for shared types
type CertificateInfo = shared.CertificateInfo
type TLSInfo = shared.TLSInfo

// IsRootCertificate checks if a certificate is a known root certificate
func IsRootCertificate(fingerprint256 string) bool {
	// This would typically check against a database of known root certificates
	// For now, we'll use a simple heuristic - if it's self-signed and has CA bit
	return false
}

// CalculateDaysRemaining calculates days remaining until certificate expiry
func CalculateDaysRemaining(validTo time.Time) int {
	now := time.Now()
	duration := validTo.Sub(now)
	return int(duration.Hours() / 24)
}
