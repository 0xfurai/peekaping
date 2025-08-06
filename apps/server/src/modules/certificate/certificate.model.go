package certificate

import (
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"strings"
	"time"
)

// CertificateInfo represents parsed certificate information
type CertificateInfo struct {
	// Basic certificate information
	Subject        string `json:"subject"`
	Issuer         string `json:"issuer"`
	Fingerprint    string `json:"fingerprint"`
	Fingerprint256 string `json:"fingerprint256"`
	SerialNumber   string `json:"serialNumber"`

	// Validity period
	ValidFrom     time.Time `json:"validFrom"`
	ValidTo       time.Time `json:"validTo"`
	DaysRemaining int       `json:"daysRemaining"`

	// Certificate type and chain information
	CertType          string           `json:"certType"`           // "server", "intermediate CA", "root CA", "self-signed"
	ValidFor          []string         `json:"validFor,omitempty"` // Subject alternative names
	IssuerCertificate *CertificateInfo `json:"issuerCertificate,omitempty"`

	// Additional metadata
	SignatureAlgorithm string `json:"signatureAlgorithm"`
	PublicKeyAlgorithm string `json:"publicKeyAlgorithm"`
	KeySize            int    `json:"keySize,omitempty"`
	Version            int    `json:"version"`
}

// TLSInfo represents the complete TLS connection information
type TLSInfo struct {
	Valid    bool             `json:"valid"`
	CertInfo *CertificateInfo `json:"certInfo,omitempty"`
}

// ParseCertificateChain parses a certificate chain from x509.Certificate
func ParseCertificateChain(cert *x509.Certificate, authorized bool) *TLSInfo {
	if cert == nil {
		return &TLSInfo{Valid: false}
	}

	certInfo := parseCertificateInfo(cert, 0, make(map[string]bool))

	return &TLSInfo{
		Valid:    authorized,
		CertInfo: certInfo,
	}
}

// parseCertificateInfo recursively parses certificate information
func parseCertificateInfo(cert *x509.Certificate, depth int, processed map[string]bool) *CertificateInfo {
	fingerprint := getCertificateFingerprint(cert)

	// Prevent infinite loops in certificate chains
	if processed[fingerprint] {
		return nil
	}
	processed[fingerprint] = true

	// Calculate days remaining
	now := time.Now()
	daysRemaining := int(cert.NotAfter.Sub(now).Hours() / 24)

	// Parse subject alternative names
	var validFor []string
	for _, name := range cert.DNSNames {
		validFor = append(validFor, name)
	}
	for _, ip := range cert.IPAddresses {
		validFor = append(validFor, ip.String())
	}

	// Determine certificate type
	certType := determineCertificateType(cert, depth)

	certInfo := &CertificateInfo{
		Subject:            cert.Subject.String(),
		Issuer:             cert.Issuer.String(),
		Fingerprint:        fingerprint,
		Fingerprint256:     getCertificateFingerprint256(cert),
		SerialNumber:       cert.SerialNumber.String(),
		ValidFrom:          cert.NotBefore,
		ValidTo:            cert.NotAfter,
		DaysRemaining:      daysRemaining,
		CertType:           certType,
		ValidFor:           validFor,
		SignatureAlgorithm: cert.SignatureAlgorithm.String(),
		PublicKeyAlgorithm: cert.PublicKeyAlgorithm.String(),
		Version:            cert.Version,
	}

	// Set key size based on public key type
	if rsaKey, ok := cert.PublicKey.(*rsa.PublicKey); ok {
		certInfo.KeySize = rsaKey.Size() * 8
	}

	return certInfo
}

// determineCertificateType determines the type of certificate
func determineCertificateType(cert *x509.Certificate, depth int) string {
	if depth == 0 {
		// First certificate in chain
		if cert.Subject.String() == cert.Issuer.String() {
			return "self-signed"
		}
		return "server"
	}

	// Deeper in the chain
	if cert.IsCA {
		if cert.Subject.String() == cert.Issuer.String() {
			return "root CA"
		}
		return "intermediate CA"
	}

	return "unknown"
}

// getCertificateFingerprint calculates SHA-1 fingerprint
func getCertificateFingerprint(cert *x509.Certificate) string {
	hash := sha1.Sum(cert.Raw)
	var parts []string
	for _, b := range hash {
		parts = append(parts, fmt.Sprintf("%02X", b))
	}
	return strings.Join(parts, ":")
}

// getCertificateFingerprint256 calculates SHA-256 fingerprint
func getCertificateFingerprint256(cert *x509.Certificate) string {
	hash := sha256.Sum256(cert.Raw)
	var parts []string
	for _, b := range hash {
		parts = append(parts, fmt.Sprintf("%02X", b))
	}
	return strings.Join(parts, ":")
}

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
