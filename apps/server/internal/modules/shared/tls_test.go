package shared_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// colonHex formats a digest the way browsers show fingerprints: AB:CD:...
func colonHex(sum []byte) string {
	return strings.ReplaceAll(fmt.Sprintf("% X", sum), " ", ":")
}

// issue signs template with the parent's key (or self-signs when parent is
// nil) and returns the parsed certificate.
func issue(t *testing.T, template, parent *x509.Certificate, pub, parentKey any) *x509.Certificate {
	t.Helper()
	if parent == nil {
		parent = template
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, pub, parentKey)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

func TestParseCertificateChain_DescribesTheServerCertificate(t *testing.T) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ca := issue(t, &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}, nil, &caKey.PublicKey, caKey)

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	notAfter := time.Now().Add(30*24*time.Hour + 12*time.Hour)
	leaf := issue(t, &x509.Certificate{
		SerialNumber: big.NewInt(4242),
		Subject:      pkix.Name{CommonName: "example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		DNSNames:     []string{"example.com", "www.example.com"},
		IPAddresses:  []net.IP{net.ParseIP("192.0.2.10")},
	}, ca, &leafKey.PublicKey, caKey)

	info := shared.ParseCertificateChain(leaf, true)

	require.NotNil(t, info.CertInfo)
	assert.True(t, info.Valid)
	cert := info.CertInfo
	assert.Equal(t, "CN=example.com", cert.Subject)
	assert.Equal(t, "CN=Test CA", cert.Issuer)
	assert.Equal(t, "server", cert.CertType)
	assert.Equal(t, 30, cert.DaysRemaining)
	assert.Equal(t, "4242", cert.SerialNumber)
	assert.Equal(t, []string{"example.com", "www.example.com", "192.0.2.10"}, cert.ValidFor)
	assert.Equal(t, 2048, cert.KeySize)
	assert.True(t, notAfter.Truncate(time.Second).Equal(cert.ValidTo))

	sum256 := sha256.Sum256(leaf.Raw)
	sum1 := sha1.Sum(leaf.Raw)
	assert.Equal(t, colonHex(sum256[:]), cert.Fingerprint256)
	assert.Equal(t, colonHex(sum1[:]), cert.Fingerprint)
	assert.Len(t, strings.Split(cert.Fingerprint256, ":"), 32)
}

func TestParseCertificateChain_SelfSignedCertificate(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cert := issue(t, &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10*24*time.Hour + time.Hour),
	}, nil, &key.PublicKey, key)

	info := shared.ParseCertificateChain(cert, false)

	require.NotNil(t, info.CertInfo)
	assert.False(t, info.Valid)
	assert.Equal(t, "self-signed", info.CertInfo.CertType)
	assert.Equal(t, 10, info.CertInfo.DaysRemaining)
}

func TestParseCertificateChain_NoCertificate(t *testing.T) {
	info := shared.ParseCertificateChain(nil, true)

	assert.False(t, info.Valid)
	assert.Nil(t, info.CertInfo)
}
