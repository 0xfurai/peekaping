package apitest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type proxyResponse struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

func TestProxiesAPI_ManagesProxy(t *testing.T) {
	a := newAppWithAdmin(t)
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody("proxy.example"), a.withAPIKey), http.StatusCreated).ID
	path := "/api/v1/proxies/" + id

	got := decode[proxyResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, proxyResponse{ID: id, Protocol: "http", Host: "proxy.example", Port: 8080}, got)
	list := decode[[]proxyResponse](t, a.request(t, http.MethodGet, "/api/v1/proxies", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, id, list[0].ID)

	decode[any](t, a.request(t, http.MethodPut, path, proxyBody("egress.example"), a.asAdmin), http.StatusOK)
	decode[any](t, a.request(t, http.MethodPatch, path, map[string]any{"port": 3128}, a.asAdmin), http.StatusOK)
	got = decode[proxyResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, proxyResponse{ID: id, Protocol: "http", Host: "egress.example", Port: 3128}, got)

	decode[any](t, a.request(t, http.MethodDelete, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, a.asAdmin).Code)
}

func TestProxiesAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody("proxy.example"), a.asAdmin), http.StatusCreated).ID
	path := "/api/v1/proxies/" + id

	proxy := proxyBody("proxy.example")
	a.expectStatus(t, http.StatusBadRequest, []call{
		{"malformed JSON", http.MethodPost, "/api/v1/proxies", "{"},
		{"unknown protocol", http.MethodPost, "/api/v1/proxies", with(proxy, "protocol", "gopher")},
		{"no host", http.MethodPost, "/api/v1/proxies", with(proxy, "host", "")},
		{"port out of range", http.MethodPost, "/api/v1/proxies", with(proxy, "port", 70000)},
		{"auth without credentials", http.MethodPost, "/api/v1/proxies", with(proxy, "auth", true)},
		{"full update with an unknown protocol", http.MethodPut, path, with(proxy, "protocol", "gopher")},
		{"partial update with a port out of range", http.MethodPatch, path, map[string]any{"port": 70000}},
		{"partial update with an unknown protocol", http.MethodPatch, path, map[string]any{"protocol": "gopher"}},
		{"negative page", http.MethodGet, "/api/v1/proxies?page=-1", nil},
	})

	got := decode[proxyResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, proxyResponse{ID: id, Protocol: "http", Host: "proxy.example", Port: 8080}, got)
}
