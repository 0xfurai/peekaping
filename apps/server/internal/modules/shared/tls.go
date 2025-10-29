package shared

import (
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
