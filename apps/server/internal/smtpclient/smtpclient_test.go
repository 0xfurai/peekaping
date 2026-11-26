package smtpclient_test

import (
	"context"
	"net/smtp"
	"testing"

	"peekaping/internal/smtpclient"
	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSend_DeliversTheMessageWithItsEnvelope(t *testing.T) {
	server := smtptest.Start(t)

	err := smtpclient.Send(context.Background(),
		smtpclient.Server{Host: server.Host(), Port: server.Port(), Auth: smtp.PlainAuth("", "user", "pass", server.Host()), TLS: smtpclient.None},
		"from@example.com", []string{"a@example.com", "b@example.com"}, []byte("Subject: Hi\r\n\r\nHello\r\n"))
	require.NoError(t, err)

	messages := server.WaitFor(t, 1)
	assert.Equal(t, "from@example.com", messages[0].From)
	assert.Equal(t, []string{"a@example.com", "b@example.com"}, messages[0].To)
	assert.Contains(t, messages[0].Data, "Hello")
	assert.True(t, messages[0].Authenticated)
}

func TestSend_StartTLSModeFailsWhenTheServerHasNoSTARTTLS(t *testing.T) {
	server := smtptest.Start(t)

	err := smtpclient.Send(context.Background(),
		smtpclient.Server{Host: server.Host(), Port: server.Port(), TLS: smtpclient.StartTLS},
		"from@example.com", []string{"a@example.com"}, []byte("Subject: Hi\r\n\r\nHello\r\n"))

	assert.ErrorContains(t, err, "STARTTLS")
	assert.Empty(t, server.Messages(), "nothing is sent in the clear when the mode asks for TLS")
}

func TestSend_OpportunisticModeSendsInTheClearWhenTheServerHasNoSTARTTLS(t *testing.T) {
	server := smtptest.Start(t)

	err := smtpclient.Send(context.Background(),
		smtpclient.Server{Host: server.Host(), Port: server.Port()},
		"from@example.com", []string{"a@example.com"}, []byte("Subject: Hi\r\n\r\nHello\r\n"))

	require.NoError(t, err)
	assert.Len(t, server.WaitFor(t, 1), 1)
}
