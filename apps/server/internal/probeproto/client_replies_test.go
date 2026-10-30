package probeproto_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ABodyThatCanNotBeEncodedIsNotSent(t *testing.T) {
	sent := false
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { sent = true })

	got, err := client.Results(context.Background(), &probeproto.ResultsRequest{Results: []probeproto.Result{{ID: "r1", TLSInfo: json.RawMessage("{")}}})

	require.Error(t, err)
	assert.Nil(t, got)
	var failure *probeproto.Error
	assert.NotErrorAs(t, err, &failure, "no reply came of it")
	assert.False(t, sent)
}

func TestClient_AnAddressThatIsNotAURLIsNotRequested(t *testing.T) {
	client := probeproto.NewClient("http://bad host", probeToken, instanceID, nil)

	got, err := client.Hello(context.Background(), &probeproto.HelloRequest{})

	require.Error(t, err)
	assert.Nil(t, got)
	var failure *probeproto.Error
	assert.NotErrorAs(t, err, &failure)
}

func TestClient_ReadsTheStatusOfAReplyBeforeItsBody(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantStatus int // of the *Error, 0 for none
		wantMsg    string
	}{
		{"a 299 is a success", 299, 0, ""},
		{"a 300 is not", http.StatusMultipleChoices, 300, "Multiple Choices"},
		{"a 304 answers only the long poll", http.StatusNotModified, 304, "Not Modified"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				reply(w, tc.status, probeproto.HelloResponse{ServerVersion: "0.0.47"}, false)
			})

			got, err := client.Hello(context.Background(), &probeproto.HelloRequest{})

			if tc.wantStatus == 0 {
				require.NoError(t, err)
				assert.Equal(t, "0.0.47", got.ServerVersion)
				return
			}
			var failure *probeproto.Error
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, tc.wantStatus, failure.Status)
			assert.Equal(t, tc.wantMsg, failure.Message)
			assert.Nil(t, got)
		})
	}
}

func TestClient_AReplyWithoutABodyLeavesTheAnswerEmpty(t *testing.T) {
	t.Run("a 204 to hello", func(t *testing.T) {
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

		got, err := client.Hello(context.Background(), &probeproto.HelloRequest{})

		require.NoError(t, err)
		assert.Equal(t, &probeproto.HelloResponse{}, got)
	})
	t.Run("a body to bye is not read", func(t *testing.T) {
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not json at all"))
		})

		assert.NoError(t, client.Bye(context.Background()))
	})
}

func TestClient_AReplyThatCanNotBeReadSaysWhichCall(t *testing.T) {
	t.Run("a gzip that is not gzip", func(t *testing.T) {
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write([]byte("this is plain text"))
		})

		got, err := client.Hello(context.Background(), &probeproto.HelloRequest{})

		assert.Nil(t, got)
		assert.ErrorContains(t, err, "probeproto: reading a gzipped reply")
	})
	t.Run("a body that is not what was asked for", func(t *testing.T) {
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"version":`)) })

		got, err := client.Assignments(context.Background(), "v1")

		assert.Nil(t, got)
		assert.ErrorContains(t, err, "probeproto: decoding the reply of GET /probe/v1/assignments?version=v1: ")
	})
	t.Run("a body that is not what was asked for to a post", func(t *testing.T) {
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[1,2]`)) })

		_, err := client.Results(context.Background(), &probeproto.ResultsRequest{})

		assert.ErrorContains(t, err, "probeproto: decoding the reply of POST /probe/v1/results: ")
	})
}

func TestClient_AnErrorStatusFromTheServerCarriesItsStatusWhateverTheMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	client := probeproto.NewClient(server.URL, probeToken, instanceID, server.Client())

	_, helloErr := client.Hello(context.Background(), &probeproto.HelloRequest{})
	_, pollErr := client.Assignments(context.Background(), "")

	for _, err := range []error{helloErr, pollErr, client.Bye(context.Background())} {
		var failure *probeproto.Error
		require.ErrorAs(t, err, &failure)
		assert.Equal(t, 500, failure.Status)
	}
}
