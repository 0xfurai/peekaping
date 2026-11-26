package executor

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRedisExecutor_configureTLS_Result(t *testing.T) {
	pki := generateTestCerts(t, time.Now().Add(24*time.Hour))

	tests := []struct {
		name         string
		cfg          RedisConfig
		wantNoTLS    bool
		wantSkip     bool
		wantRootCAs  bool
		wantCertsLen int
		wantErr      string
	}{
		{name: "redis scheme has no TLS", cfg: RedisConfig{DatabaseConnectionString: "redis://localhost:6379", IgnoreTls: true, CaCert: "garbage"}, wantNoTLS: true},
		{name: "unparsable connection string", cfg: RedisConfig{DatabaseConnectionString: "rediss://local\nhost:6379"}, wantErr: `failed to parse connection string: parse "rediss://local\nhost:6379": net/url: invalid control character in URL`},
		{name: "ignore TLS skips verification and loads nothing", cfg: RedisConfig{DatabaseConnectionString: "rediss://localhost:6379", IgnoreTls: true, CaCert: "garbage", ClientCert: "garbage"}, wantSkip: true},
		{name: "no certificates", cfg: RedisConfig{DatabaseConnectionString: "rediss://localhost:6379"}},
		{name: "CA certificate", cfg: RedisConfig{DatabaseConnectionString: "rediss://localhost:6379", CaCert: pki.caPEM}, wantRootCAs: true},
		{name: "client certificate", cfg: RedisConfig{DatabaseConnectionString: "rediss://localhost:6379", ClientCert: pki.clientCertPEM, ClientKey: pki.clientKeyPEM}, wantCertsLen: 1},
		{name: "CA and client certificate", cfg: RedisConfig{DatabaseConnectionString: "rediss://localhost:6379", CaCert: pki.caPEM, ClientCert: pki.clientCertPEM, ClientKey: pki.clientKeyPEM}, wantRootCAs: true, wantCertsLen: 1},
		{name: "CA that is not a certificate", cfg: RedisConfig{DatabaseConnectionString: "rediss://localhost:6379", CaCert: "garbage"}, wantErr: "failed to load CA certificate: failed to parse CA certificate"},
		{name: "client certificate without a key", cfg: RedisConfig{DatabaseConnectionString: "rediss://localhost:6379", ClientCert: pki.clientCertPEM}, wantErr: "failed to load client certificate: both client certificate and key must be provided"},
		{name: "client key without a certificate", cfg: RedisConfig{DatabaseConnectionString: "rediss://localhost:6379", ClientKey: pki.clientKeyPEM}, wantErr: "failed to load client certificate: both client certificate and key must be provided"},
		{name: "client certificate that is not PEM", cfg: RedisConfig{DatabaseConnectionString: "rediss://localhost:6379", ClientCert: "garbage", ClientKey: "garbage"}, wantErr: "failed to load client certificate: failed to load client certificate: tls: failed to find any PEM data in certificate input"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &redis.Options{}

			err := NewRedisExecutor(zap.NewNop().Sugar()).configureTLS(&tt.cfg, opts)

			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
				assert.Nil(t, opts.TLSConfig)
				return
			}
			require.NoError(t, err)
			if tt.wantNoTLS {
				assert.Nil(t, opts.TLSConfig)
				return
			}
			require.NotNil(t, opts.TLSConfig)
			assert.Equal(t, tt.wantSkip, opts.TLSConfig.InsecureSkipVerify)
			assert.Equal(t, tt.wantRootCAs, opts.TLSConfig.RootCAs != nil)
			assert.Len(t, opts.TLSConfig.Certificates, tt.wantCertsLen)
		})
	}
}

func TestIsValidHostname(t *testing.T) {
	tests := []struct {
		hostname string
		want     bool
	}{
		{"", false},
		{"localhost", true},
		{"redis.example.com", true},
		{"redis-1.example.com", true},
		{"R3dis.Example.COM", true},
		{"192.168.1.1", true},
		{"a", true},
		{"a-b", true},
		{"a.", false},
		{".a", false},
		{"a..b", false},
		{"-a.com", false},
		{"a-.com", false},
		{"a.-b.com", false},
		{"a.b-.com", false},
		{"under_score.com", false},
		{"with space", false},
		{"emoji🙂.com", false},
		{"ünï.com", false},
		{"a/b", false},
		{"::1", true},
		{"2001:db8::1", true},
		{"[::1]", true},
		{"[2001:db8::1]", true},
		{"[::1", false},
		{"::1]", false},
		{"[[::1]]", true},
		{"[not:ipv6]", false},
		{"not:ipv6", false},
		{"::ffff:192.0.2.1", false},
		{"[::ffff:192.0.2.1]", false},
		{"192.0.2.1:6379", false},
		{"[]", false},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidHostname(tt.hostname))
		})
	}
}
