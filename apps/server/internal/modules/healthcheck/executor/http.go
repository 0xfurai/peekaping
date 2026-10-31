package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"peekaping/internal/hostport"
	"peekaping/internal/modules/shared"
	"peekaping/internal/netguard"
	"peekaping/internal/validation"
	"peekaping/internal/version"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto/tls"
	"crypto/x509"

	"github.com/Azure/go-ntlmssp"
	"github.com/go-playground/validator/v10"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"golang.org/x/net/proxy"
)

func HTTPConfigStructLevelValidation(sl validator.StructLevel) {
	cfg := sl.Current().Interface().(HTTPConfig)

	validateHTTPBody(sl, cfg)

	// Authentication validation
	if validate, ok := httpAuthValidators[cfg.AuthMethod]; ok {
		validate(sl, cfg)
	}
}

// httpBodyParsers parse a body of each encoding that has a format to check. The "text" encoding
// needs no validation.
var httpBodyParsers = map[string]func(body string) error{
	"json": func(body string) error {
		var js json.RawMessage
		return json.Unmarshal([]byte(body), &js)
	},
	"form": func(body string) error {
		_, err := url.ParseQuery(body)
		return err
	},
	"xml": func(body string) error {
		return xml.Unmarshal([]byte(body), new(interface{}))
	},
}

// validateHTTPBody reports a body that isn't in the format of its encoding
func validateHTTPBody(sl validator.StructLevel, cfg HTTPConfig) {
	parse, ok := httpBodyParsers[cfg.Encoding]
	if !ok || cfg.Body == "" {
		return
	}
	if err := parse(cfg.Body); err != nil {
		sl.ReportError(cfg.Body, "Body", "body", cfg.Encoding, "")
	}
}

// httpAuthValidators check the fields each auth method requires. The "none" method requires none.
var httpAuthValidators = map[string]func(sl validator.StructLevel, cfg HTTPConfig){
	"none":      func(sl validator.StructLevel, cfg HTTPConfig) {},
	"basic":     validateBasicAuth,
	"ntlm":      validateNTLMAuth,
	"oauth2-cc": validateOAuth2CCAuth,
	"mtls":      validateMTLSAuth,
}

// requireHTTPField reports a field that is empty
func requireHTTPField(sl validator.StructLevel, value, fieldName, jsonName, tag string) {
	if value == "" {
		sl.ReportError(value, fieldName, jsonName, tag, "")
	}
}

func validateBasicAuth(sl validator.StructLevel, cfg HTTPConfig) {
	requireHTTPField(sl, cfg.BasicAuthUser, "BasicAuthUser", "basic_auth_user", "required_with_auth_basic")
	requireHTTPField(sl, cfg.BasicAuthPass, "BasicAuthPass", "basic_auth_pass", "required_with_auth_basic")
}

func validateNTLMAuth(sl validator.StructLevel, cfg HTTPConfig) {
	requireHTTPField(sl, cfg.BasicAuthUser, "BasicAuthUser", "basic_auth_user", "required_with_auth_ntlm")
	requireHTTPField(sl, cfg.BasicAuthPass, "BasicAuthPass", "basic_auth_pass", "required_with_auth_ntlm")
	requireHTTPField(sl, cfg.AuthDomain, "AuthDomain", "authDomain", "required_with_auth_ntlm")
	requireHTTPField(sl, cfg.AuthWorkstation, "AuthWorkstation", "authWorkstation", "required_with_auth_ntlm")
}

func validateOAuth2CCAuth(sl validator.StructLevel, cfg HTTPConfig) {
	if cfg.OauthAuthMethod != "client_secret_basic" && cfg.OauthAuthMethod != "client_secret_post" {
		sl.ReportError(cfg.OauthAuthMethod, "OauthAuthMethod", "oauth_auth_method", "oneof=client_secret_basic client_secret_post", "")
	}
	if cfg.OauthTokenUrl == "" {
		sl.ReportError(cfg.OauthTokenUrl, "OauthTokenUrl", "oauth_token_url", "required_with_auth_oauth2cc", "")
	} else if _, err := url.ParseRequestURI(cfg.OauthTokenUrl); err != nil {
		sl.ReportError(cfg.OauthTokenUrl, "OauthTokenUrl", "oauth_token_url", "url", "")
	}
	requireHTTPField(sl, cfg.OauthClientId, "OauthClientId", "oauth_client_id", "required_with_auth_oauth2cc")
	requireHTTPField(sl, cfg.OauthClientSecret, "OauthClientSecret", "oauth_client_secret", "required_with_auth_oauth2cc")
	// OauthScopes is optional
}

func validateMTLSAuth(sl validator.StructLevel, cfg HTTPConfig) {
	requireHTTPField(sl, cfg.TlsCert, "TlsCert", "tlsCert", "required_with_auth_mtls")
	requireHTTPField(sl, cfg.TlsKey, "TlsKey", "tlsKey", "required_with_auth_mtls")
	// TlsCa is optional
}

// defaultMaxRedirects is how many redirects a check follows when its config sets no max_redirects
const defaultMaxRedirects = 10

type HTTPConfig struct {
	Url string `json:"url" validate:"required,url"`

	Method              string   `json:"method" validate:"required,oneof=GET POST PUT DELETE PATCH HEAD OPTIONS"`
	Headers             string   `json:"headers" validate:"omitempty,json"`
	Encoding            string   `json:"encoding" validate:"required,oneof=json form xml text"`
	Body                string   `json:"body" validate:"omitempty"`
	AcceptedStatusCodes []string `json:"accepted_statuscodes" validate:"required,dive,oneof=2XX 3XX 4XX 5XX"`
	MaxRedirects        *int     `json:"max_redirects,omitempty" validate:"omitempty,min=0"` // nil: defaultMaxRedirects; 0: follow none
	IgnoreTlsErrors     bool     `json:"ignore_tls_errors"`
	CheckCertExpiry     bool     `json:"check_cert_expiry"`

	// Response validation fields
	Keyword       string `json:"keyword,omitempty"`
	InvertKeyword bool   `json:"invert_keyword,omitempty"`
	JsonQuery     string `json:"json_query,omitempty"`
	JsonCondition string `json:"json_condition,omitempty" validate:"omitempty,oneof='==' '!=' '>' '<' '>=' '<='"`
	ExpectedValue string `json:"expected_value,omitempty"`

	// Authentication fields
	AuthMethod        string `json:"authMethod" validate:"required,oneof=none basic oauth2-cc ntlm mtls"`
	BasicAuthUser     string `json:"basic_auth_user,omitempty"`
	BasicAuthPass     string `json:"basic_auth_pass,omitempty"`
	AuthDomain        string `json:"authDomain,omitempty"`
	AuthWorkstation   string `json:"authWorkstation,omitempty"`
	OauthAuthMethod   string `json:"oauth_auth_method,omitempty"`
	OauthTokenUrl     string `json:"oauth_token_url,omitempty"`
	OauthClientId     string `json:"oauth_client_id,omitempty"`
	OauthClientSecret string `json:"oauth_client_secret,omitempty"`
	OauthScopes       string `json:"oauth_scopes,omitempty"`
	TlsCert           string `json:"tlsCert,omitempty"`
	TlsKey            string `json:"tlsKey,omitempty"`
	TlsCa             string `json:"tlsCa,omitempty"`
}

type HTTPExecutor struct {
	guarded
	logger *zap.SugaredLogger
}

// TLSInterceptor is a custom RoundTripper that captures TLS certificate information
type TLSInterceptor struct {
	transport http.RoundTripper
	tlsInfo   *shared.TLSInfo
	mutex     sync.RWMutex
}

func NewTLSInterceptor(transport http.RoundTripper) *TLSInterceptor {
	return &TLSInterceptor{
		transport: transport,
	}
}

func (t *TLSInterceptor) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.transport.RoundTrip(req)

	// Try to extract TLS information if this is an HTTPS request
	if err == nil && resp != nil && resp.TLS != nil {
		tlsInfo := t.extractTLSInfo(resp.TLS)
		t.mutex.Lock()
		t.tlsInfo = tlsInfo
		t.mutex.Unlock()
	}

	return resp, err
}

func (t *TLSInterceptor) extractTLSInfo(tlsState *tls.ConnectionState) *shared.TLSInfo {
	if len(tlsState.PeerCertificates) == 0 {
		return &shared.TLSInfo{Valid: false}
	}

	// Get the server certificate (first in the chain)
	serverCert := tlsState.PeerCertificates[0]

	// Check if the certificate chain is verified
	verified := len(tlsState.VerifiedChains) > 0

	return shared.ParseCertificateChain(serverCert, verified)
}

func (t *TLSInterceptor) GetTLSInfo() *shared.TLSInfo {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.tlsInfo
}

func (t *TLSInterceptor) Reset() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.tlsInfo = nil
}

// registerConfigValidation registers the config's rule on the shared validator once. The
// validator's Register methods are not safe while another goroutine validates, and the API tests
// build many servers in one process while others check monitors.
var registerConfigValidation sync.Once

func NewHTTPExecutor(logger *zap.SugaredLogger) *HTTPExecutor {
	registerConfigValidation.Do(func() {
		validation.Validate.RegisterStructValidation(HTTPConfigStructLevelValidation, HTTPConfig{})
	})

	return &HTTPExecutor{
		logger: logger,
	}
}

func (s *HTTPExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[HTTPConfig](configJSON)
}

func (s *HTTPExecutor) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*HTTPConfig))
}

// Targets lists the hosts an HTTP check dials: the URL's, and the OAuth token URL's when the
// monitor authenticates with one. A proxy is not listed: it is a record of its own, which is
// checked when it is saved.
func (s *HTTPExecutor) Targets(configJSON string) ([]string, error) {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return nil, err
	}
	httpCfg := cfg.(*HTTPConfig)
	var hosts []string
	for _, raw := range []string{httpCfg.Url, httpCfg.OauthTokenUrl} {
		if raw == "" {
			continue
		}
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			hosts = append(hosts, u.Hostname())
		}
	}
	return hosts, nil
}

// guardTransport makes t dial through the guard. The guard may be off now: it is asked at each
// dial, so a guard turned on while a check runs refuses the check's request and its redirects too,
// and off it lets every dial through. A transport that has a dial function of its own stops trying
// HTTP/2 unless asked to, so it is asked to where it tried it before: when it has no TLS config of
// its own.
func guardTransport(t *http.Transport, guard *netguard.Guard) {
	if guard == nil {
		return
	}
	if t.TLSClientConfig == nil {
		t.ForceAttemptHTTP2 = true
	}
	t.DialContext = guard.DialContext
}

// guardedDirect is the dialer a SOCKS proxy connects to its own server with, which is the only
// connection the check makes itself: the proxy dials the target.
type guardedDirect struct{ guard *netguard.Guard }

func (d guardedDirect) Dial(network, addr string) (net.Conn, error) {
	return d.guard.DialContext(context.Background(), network, addr)
}

func (d guardedDirect) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.guard.DialContext(ctx, network, addr)
}

// guardedClient is a client for a request that isn't the check's own, such as the OAuth token
// request. It is built for the one check, and dials through the guard, which may be turned on while
// the check runs. With no guard it is http.DefaultClient, as the request used before. While the
// guard is on, the target is dialed itself, and not through the proxy that HTTP_PROXY and
// HTTPS_PROXY name: the guard would check the proxy's address then, and not the target's. It is
// asked at each request, so a redirect after the guard was turned on goes without the proxy too.
func guardedClient(guard *netguard.Guard) (*http.Client, func()) {
	if guard == nil {
		return http.DefaultClient, func() {}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		if guard.Enabled() {
			return nil, nil
		}
		return http.ProxyFromEnvironment(req)
	}
	guardTransport(transport, guard)
	return &http.Client{Transport: transport}, transport.CloseIdleConnections
}

// statusPatternRanges maps each accepted status pattern to the codes it covers, from the first to
// the one after the last
var statusPatternRanges = map[string][2]int{
	"2XX": {200, 300},
	"3XX": {300, 400},
	"4XX": {400, 500},
	"5XX": {500, 600},
}

// Helper to check if status code matches accepted patterns
func isStatusAccepted(statusCode int, accepted []string) bool {
	for _, pattern := range accepted {
		if codes, ok := statusPatternRanges[pattern]; ok && statusCode >= codes[0] && statusCode < codes[1] {
			return true
		}
	}
	return false
}

// Helper to check keyword in response body
func checkKeyword(responseBody, keyword string, invert bool) bool {
	if keyword == "" {
		return true // No keyword check needed
	}

	found := strings.Contains(responseBody, keyword)
	if invert {
		return !found
	}
	return found
}

// Helper to check JSON query and expected value
func checkJsonQuery(responseBody, jsonQuery, condition, expectedValue string) (bool, error) {
	if jsonQuery == "" && expectedValue == "" && condition == "" {
		return true, nil // No JSON query check needed
	}

	var result gjson.Result

	if jsonQuery == "" {
		// When no query is specified, we want to compare the full response
		if expectedValue != "" {
			return compareFullResponse(responseBody, condition, expectedValue)
		}

		// If no expected value, just return the raw response body as string for backward compatibility
		result = gjson.Result{Type: gjson.String, Str: responseBody}
	} else {
		result = gjson.Get(responseBody, jsonQuery)
		if !result.Exists() {
			return false, fmt.Errorf("JSON query path not found: %s", jsonQuery)
		}
	}

	if expectedValue == "" && condition == "" {
		return result.Exists(), nil
	}

	return compareQueryResult(result.String(), condition, expectedValue)
}

// compareFullResponse compares the whole response to the expected value: as JSON when both are
// JSON, and as text otherwise. Only equality is supported for either.
func compareFullResponse(responseBody, condition, expectedValue string) (bool, error) {
	var responseJson, expectedJson interface{}

	// Try to parse both as JSON first
	responseIsJson := json.Unmarshal([]byte(responseBody), &responseJson) == nil
	expectedIsJson := json.Unmarshal([]byte(expectedValue), &expectedJson) == nil

	// If both are valid JSON, do JSON comparison
	if responseIsJson && expectedIsJson {
		return compareNormalizedJSON(responseJson, expectedJson, condition)
	}

	// If one or both are not valid JSON, fall back to string comparison
	// But only allow == and != conditions for non-JSON comparison
	return compareEquality(responseBody, expectedValue, condition, "non-JSON")
}

// compareNormalizedJSON compares two parsed JSON values, as the normalized JSON they marshal to
func compareNormalizedJSON(responseJson, expectedJson interface{}, condition string) (bool, error) {
	// For JSON comparison, we need to marshal both back to normalized JSON strings
	// This ensures consistent formatting and key ordering
	responseJsonBytes, err := json.Marshal(responseJson)
	if err != nil {
		return false, fmt.Errorf("failed to marshal response JSON: %w", err)
	}

	expectedJsonBytes, err := json.Marshal(expectedJson)
	if err != nil {
		return false, fmt.Errorf("failed to marshal expected JSON: %w", err)
	}

	// Compare the normalized JSON strings
	return compareEquality(string(responseJsonBytes), string(expectedJsonBytes), condition, "full JSON")
}

// compareEquality applies the == or != condition to two values. The condition is == when empty.
// kind names what is compared, for the error of any other condition.
func compareEquality(actualValue, expectedValue, condition, kind string) (bool, error) {
	// Default condition is equality
	if condition == "" {
		condition = "=="
	}

	switch condition {
	case "==":
		return actualValue == expectedValue, nil
	case "!=":
		return actualValue != expectedValue, nil
	default:
		return false, fmt.Errorf("unsupported condition '%s' for %s comparison: only '==' and '!=' are supported", condition, kind)
	}
}

// orderedComparison is a condition that orders two values: how it compares two numbers, and how it
// reads the result of comparing two strings
type orderedComparison struct {
	numbers func(actual, expected float64) bool
	strings func(compared int) bool
}

var orderedComparisons = map[string]orderedComparison{
	">":  {func(a, e float64) bool { return a > e }, func(c int) bool { return c > 0 }},
	"<":  {func(a, e float64) bool { return a < e }, func(c int) bool { return c < 0 }},
	">=": {func(a, e float64) bool { return a >= e }, func(c int) bool { return c >= 0 }},
	"<=": {func(a, e float64) bool { return a <= e }, func(c int) bool { return c <= 0 }},
}

// compareQueryResult applies the condition to the value a JSON query found. The condition is ==
// when empty. Numbers compare as numbers, and anything else as strings.
func compareQueryResult(actualValue, condition, expectedValue string) (bool, error) {
	if condition == "" {
		condition = "=="
	}

	switch condition {
	case "==":
		return actualValue == expectedValue, nil
	case "!=":
		return actualValue != expectedValue, nil
	}

	comparison, ok := orderedComparisons[condition]
	if !ok {
		return false, fmt.Errorf("unsupported condition: %s", condition)
	}

	actualFloat, err1 := strconv.ParseFloat(actualValue, 64)
	expectedFloat, err2 := strconv.ParseFloat(expectedValue, 64)
	if err1 != nil || err2 != nil {
		return comparison.strings(strings.Compare(actualValue, expectedValue)), nil
	}
	return comparison.numbers(actualFloat, expectedFloat), nil
}

func buildProxyTransport(base *http.Transport, proxyModel *Proxy) http.RoundTripper {
	return buildGuardedTransport(base, proxyModel, nil)
}

// buildGuardedTransport is buildProxyTransport for a guard that may be on. The connection the
// check makes is the one to the proxy, or to the target when there is none, and the guard checks
// it: a proxy at a blocked address is refused. What an HTTP proxy then dials is its own business.
func buildGuardedTransport(base *http.Transport, proxyModel *Proxy, guard *netguard.Guard) http.RoundTripper {
	guardTransport(base, guard)
	if proxyModel == nil {
		return base
	}

	// Set default protocol if not specified
	protocol := proxyModel.Protocol
	if protocol == "" {
		protocol = "http"
	}

	switch protocol {
	case "http", "https":
		return httpProxyTransport(base, proxyModel, protocol)
	case "socks", "socks5", "socks5h", "socks4":
		return socksProxyTransport(base, proxyModel, guard)
	default:
		return base
	}
}

// hasProxyCredentials reports whether the proxy needs a username and password
func hasProxyCredentials(proxyModel *Proxy) bool {
	return proxyModel.Auth && proxyModel.Username != "" && proxyModel.Password != ""
}

// httpProxyTransport sends the transport's requests through an HTTP or HTTPS proxy
func httpProxyTransport(base *http.Transport, proxyModel *Proxy, protocol string) http.RoundTripper {
	proxyURL := &url.URL{
		Scheme: protocol,
		Host:   hostport.Join(proxyModel.Host, proxyModel.Port),
	}
	if hasProxyCredentials(proxyModel) {
		proxyURL.User = url.UserPassword(proxyModel.Username, proxyModel.Password)
	}
	base.Proxy = http.ProxyURL(proxyURL)
	return base
}

// socksProxyTransport has the transport dial through a SOCKS proxy
func socksProxyTransport(base *http.Transport, proxyModel *Proxy, guard *netguard.Guard) http.RoundTripper {
	var auth *proxy.Auth
	if hasProxyCredentials(proxyModel) {
		auth = &proxy.Auth{
			User:     proxyModel.Username,
			Password: proxyModel.Password,
		}
	}
	address := hostport.Join(proxyModel.Host, proxyModel.Port)
	var forward proxy.Dialer = proxy.Direct
	if guard != nil {
		forward = guardedDirect{guard}
	}
	dialer, err := proxy.SOCKS5("tcp", address, auth, forward)
	if err != nil {
		// fallback to default transport if dialer fails
		return base
	}
	base.DialContext = func(ctx context.Context, network, addr string) (conn net.Conn, e error) {
		if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
			return contextDialer.DialContext(ctx, network, addr)
		}
		return dialer.Dial(network, addr)
	}
	base.Proxy = nil // No HTTP proxy
	return base
}

func setDefaultHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "peekaping/"+version.Version)
	req.Header.Set("Accept", "*/*")
}

// isLegacyDefaultContentType reports whether headers are exactly the singleton
// Content-Type: application/json header older web forms wrote on every HTTP
// monitor. It doesn't describe a bodyless request, and some servers reject it,
// so the executor drops it when there is no body (issue #395). Monitors with
// any other content type, casing, whitespace, or additional headers keep their
// header.
func isLegacyDefaultContentType(headers map[string]string, body string) bool {
	if body != "" || len(headers) != 1 {
		return false
	}
	for key, value := range headers {
		if key == "Content-Type" && value == "application/json" {
			return true
		}
	}
	return false
}

func (h *HTTPExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := h.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now(), time.Now())
	}
	cfg := cfgAny.(*HTTPConfig)

	req, err := newHTTPRequest(ctx, cfg)
	if err != nil {
		return DownResult(err, time.Now(), time.Now())
	}

	// --- PROXY LOGIC ---

	// Default transport with proxy if needed
	baseTransport := &http.Transport{}
	// A fresh transport is built per execution; close its idle keep-alive
	// connections before returning so checks don't leak one socket each.
	defer baseTransport.CloseIdleConnections()

	// Configure TLS settings if needed
	if cfg.IgnoreTlsErrors {
		baseTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	transport := buildGuardedTransport(baseTransport, proxyModel, h.guard)

	// Create TLS interceptor to capture certificate information
	tlsInterceptor := NewTLSInterceptor(transport)

	// Built for this check only: the executor is shared by every concurrent check.
	sender := &httpSender{
		client: &http.Client{
			Timeout:       time.Duration(m.Timeout) * time.Second,
			CheckRedirect: h.redirectPolicy(cfg.maxRedirects()),
			Transport:     tlsInterceptor,
		},
		interceptor: tlsInterceptor,
		cleanup:     func() {},
	}

	// --- AUTHENTICATION LOGIC ---
	if err := h.authenticate(ctx, req, cfg, proxyModel, sender); err != nil {
		return DownResult(err, time.Now(), time.Now())
	}
	defer sender.close()

	startTime := time.Now()
	resp, err := sender.client.Do(req)
	endTime := time.Now()

	if err != nil {
		h.logger.Infof("HTTP request failed: %s, %s", m.Name, err.Error())
		result := DownResult(err, startTime, endTime)
		// Try to get TLS info even on error for HTTPS requests
		result.TLSInfo = httpTLSInfo(cfg.Url, sender.interceptor)
		return result
	}
	defer resp.Body.Close()

	h.logger.Infof("HTTP response status: %s, %d", m.Name, resp.StatusCode)

	// Extract TLS information if available
	tlsInfo := httpTLSInfo(cfg.Url, sender.interceptor)

	return h.evaluateResponse(m, cfg, resp, startTime, endTime, tlsInfo)
}

// newHTTPRequest builds the request the check sends, with its headers and body
func newHTTPRequest(ctx context.Context, cfg *HTTPConfig) (*http.Request, error) {
	var bodyReader io.Reader
	if cfg.Body != "" {
		bodyReader = bytes.NewReader([]byte(cfg.Body))
	}

	req, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.Url, bodyReader)
	if err != nil {
		return nil, err
	}
	setDefaultHeaders(req)

	if err := applyConfiguredHeaders(req, cfg); err != nil {
		return nil, err
	}
	setInferredContentType(req, cfg)

	return req, nil
}

// applyConfiguredHeaders sets the headers the config holds, as a JSON object
func applyConfiguredHeaders(req *http.Request, cfg *HTTPConfig) error {
	if cfg.Headers == "" {
		return nil
	}

	headersMap := make(map[string]string)
	err := json.Unmarshal([]byte(cfg.Headers), &headersMap)
	if err != nil {
		return fmt.Errorf("invalid headers json: %w", err)
	}
	dropLegacyContentType := isLegacyDefaultContentType(headersMap, cfg.Body)
	for k, v := range headersMap {
		if dropLegacyContentType && strings.EqualFold(k, "Content-Type") {
			continue
		}
		req.Header.Set(k, v)
	}
	return nil
}

// contentTypeByEncoding is the Content-Type that describes a request body of each encoding
var contentTypeByEncoding = map[string]string{
	"json": "application/json",
	"form": "application/x-www-form-urlencoded",
	"xml":  "application/xml",
	"text": "text/plain",
}

// setInferredContentType sets the Content-Type of the body's encoding
func setInferredContentType(req *http.Request, cfg *HTTPConfig) {
	// Content-Type describes the request body, so only infer it when one is
	// sent. A Content-Type from the configured headers always wins.
	if cfg.Body == "" {
		return
	}
	if _, hasContentType := req.Header["Content-Type"]; hasContentType {
		return
	}
	if contentType, ok := contentTypeByEncoding[cfg.Encoding]; ok {
		req.Header.Set("Content-Type", contentType)
	}
}

// maxRedirects is how many redirects the check follows: the configured number, or the default
// when the config sets none
func (cfg *HTTPConfig) maxRedirects() int {
	if cfg.MaxRedirects == nil {
		return defaultMaxRedirects
	}
	return *cfg.MaxRedirects
}

// redirectPolicy is the client's CheckRedirect: it follows up to maxRedirects redirects. With 0 it
// follows none and returns the redirect response itself, which the accepted status codes judge.
func (h *HTTPExecutor) redirectPolicy(maxRedirects int) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		h.logger.Debugf("checkRedirect: %d redirects followed, max allowed: %d", len(via), maxRedirects)
		if maxRedirects == 0 {
			return http.ErrUseLastResponse
		}
		if len(via) > maxRedirects {
			return fmt.Errorf("too many redirects: followed %d redirects, maximum allowed is %d", len(via), maxRedirects)
		}
		return nil
	}
}

// httpSender is what sends a check's request: the client, the TLS interceptor that captures the
// certificate of the connection it makes, and a cleanup for any transport the authentication set up
type httpSender struct {
	client      *http.Client
	interceptor *TLSInterceptor
	cleanup     func()
}

func (s *httpSender) close() {
	s.cleanup()
}

// authenticate has the request carry the config's authentication. Some methods change the sender:
// NTLM and mTLS replace its client's transport.
func (h *HTTPExecutor) authenticate(ctx context.Context, req *http.Request, cfg *HTTPConfig, proxyModel *Proxy, sender *httpSender) error {
	switch cfg.AuthMethod {
	case "basic":
		req.SetBasicAuth(cfg.BasicAuthUser, cfg.BasicAuthPass)
	case "ntlm":
		// NTLM authentication using github.com/Azure/go-ntlmssp
		sender.client.Transport = &ntlmssp.Negotiator{RoundTripper: sender.interceptor}

		if cfg.AuthDomain != "" {
			req.SetBasicAuth(cfg.AuthDomain+"\\"+cfg.BasicAuthUser, cfg.BasicAuthPass)
		} else {
			req.SetBasicAuth(cfg.BasicAuthUser, cfg.BasicAuthPass)
		}
	case "oauth2-cc":
		token, err := h.fetchOAuth2Token(ctx, cfg)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	case "mtls":
		return h.setUpMTLS(cfg, proxyModel, sender)
	}
	return nil
}

// fetchOAuth2Token gets an access token from the config's token endpoint, with the client
// credentials grant
func (h *HTTPExecutor) fetchOAuth2Token(ctx context.Context, cfg *HTTPConfig) (string, error) {
	tokenReq, err := oauth2TokenRequest(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create oauth2 token request: %w", err)
	}

	tokenClient, closeTokenClient := guardedClient(h.guard)
	defer closeTokenClient()
	tokenResp, err := tokenClient.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("failed to get oauth2 token: %w", err)
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode < 200 || tokenResp.StatusCode >= 300 {
		return "", fmt.Errorf("oauth2 token endpoint returned status: %d", tokenResp.StatusCode)
	}
	var tokenData struct {
		AccessToken string `json:"access_token"`
	}
	err = json.NewDecoder(tokenResp.Body).Decode(&tokenData)
	if err != nil || tokenData.AccessToken == "" {
		return "", fmt.Errorf("failed to parse oauth2 token response: %w", err)
	}
	return tokenData.AccessToken, nil
}

// oauth2TokenRequest builds the request for the access token, with the client credentials in the
// body or in a Basic header, as the config says
func oauth2TokenRequest(ctx context.Context, cfg *HTTPConfig) (*http.Request, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	if cfg.OauthScopes != "" {
		form.Set("scope", cfg.OauthScopes)
	}
	if cfg.OauthAuthMethod == "client_secret_post" {
		form.Set("client_id", cfg.OauthClientId)
		form.Set("client_secret", cfg.OauthClientSecret)
	}

	tokenReq, err := http.NewRequestWithContext(ctx, "POST", cfg.OauthTokenUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	setDefaultHeaders(tokenReq)

	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cfg.OauthAuthMethod == "client_secret_basic" {
		basic := base64.StdEncoding.EncodeToString([]byte(cfg.OauthClientId + ":" + cfg.OauthClientSecret))
		tokenReq.Header.Set("Authorization", "Basic "+basic)
	}
	return tokenReq, nil
}

// setUpMTLS has the sender connect with the config's client certificate, over a transport of its
// own that the sender's cleanup closes
func (h *HTTPExecutor) setUpMTLS(cfg *HTTPConfig, proxyModel *Proxy, sender *httpSender) error {
	cert, err := tls.X509KeyPair([]byte(cfg.TlsCert), []byte(cfg.TlsKey))
	if err != nil {
		return fmt.Errorf("invalid mTLS cert/key: %w", err)
	}
	caCertPool, err := mtlsRootCAs(cfg.TlsCa)
	if err != nil {
		return err
	}
	mtlsTransport := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates:       []tls.Certificate{cert},
			RootCAs:            caCertPool,
			InsecureSkipVerify: cfg.IgnoreTlsErrors,
		},
	}
	sender.cleanup = mtlsTransport.CloseIdleConnections
	mtlsTransportWithProxy := buildGuardedTransport(mtlsTransport, proxyModel, h.guard)
	mtlsTLSInterceptor := NewTLSInterceptor(mtlsTransportWithProxy)
	sender.interceptor = mtlsTLSInterceptor // Update the active interceptor for mTLS
	sender.client.Transport = mtlsTLSInterceptor
	return nil
}

// mtlsRootCAs is the pool of the CA the server's certificate is verified against, or nil without one
func mtlsRootCAs(tlsCa string) (*x509.CertPool, error) {
	// With no CA, RootCAs stays nil and crypto/tls verifies the server
	// against the host's system roots.
	if strings.TrimSpace(tlsCa) == "" {
		return nil, nil
	}
	caCertPool := x509.NewCertPool()
	if ok := caCertPool.AppendCertsFromPEM([]byte(tlsCa)); !ok {
		return nil, fmt.Errorf("invalid mTLS CA cert")
	}
	return caCertPool, nil
}

// httpTLSInfo is the certificate information the interceptor captured, for an HTTPS URL
func httpTLSInfo(requestURL string, interceptor *TLSInterceptor) *shared.TLSInfo {
	if strings.HasPrefix(requestURL, "https://") && interceptor != nil {
		return interceptor.GetTLSInfo()
	}
	return nil
}

// evaluateResponse is the check's result for the response: Down for a status that isn't accepted,
// a body that can't be read, or a body that fails the keyword or JSON query check
func (h *HTTPExecutor) evaluateResponse(m *Monitor, cfg *HTTPConfig, resp *http.Response, startTime, endTime time.Time, tlsInfo *shared.TLSInfo) *Result {
	down := func(message string) *Result {
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   message,
			StartTime: startTime,
			EndTime:   endTime,
			TLSInfo:   tlsInfo,
		}
	}

	if !isStatusAccepted(resp.StatusCode, cfg.AcceptedStatusCodes) {
		return down(fmt.Sprintf("HTTP request failed with status: %d", resp.StatusCode))
	}

	// Read response body for content validation
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return down(fmt.Sprintf("Failed to read response body: %v", err))
	}
	var responseBody = string(bodyBytes)
	h.logger.Debugf("Response body length: %d", len(responseBody))

	// Check keyword if specified
	if message := httpKeywordFailure(cfg, responseBody); message != "" {
		return down(message)
	}

	// Check JSON query if specified
	if m.Type == "http-json-query" {
		if message := httpJSONQueryFailure(cfg, responseBody); message != "" {
			return down(message)
		}
	}

	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   fmt.Sprintf("%d - %s", resp.StatusCode, resp.Status),
		StartTime: startTime,
		EndTime:   endTime,
		TLSInfo:   tlsInfo,
	}
}

// httpKeywordFailure describes how the body fails the keyword check, or is empty when it passes
func httpKeywordFailure(cfg *HTTPConfig, responseBody string) string {
	if cfg.Keyword == "" || checkKeyword(responseBody, cfg.Keyword, cfg.InvertKeyword) {
		return ""
	}
	if cfg.InvertKeyword {
		return fmt.Sprintf("Keyword check failed: keyword '%s' found in response (expected absent)", cfg.Keyword)
	}
	return fmt.Sprintf("Keyword check failed: keyword '%s' not found in response", cfg.Keyword)
}

// httpJSONQueryFailure describes how the body fails the JSON query check, or is empty when it passes
func httpJSONQueryFailure(cfg *HTTPConfig, responseBody string) string {
	isValid, err := checkJsonQuery(responseBody, cfg.JsonQuery, cfg.JsonCondition, cfg.ExpectedValue)
	if err != nil {
		return fmt.Sprintf("JSON query validation error: %v", err)
	}
	if isValid {
		return ""
	}
	condition := cfg.JsonCondition
	if condition == "" {
		condition = "=="
	}
	return fmt.Sprintf("JSON query validation failed: query '%s' with condition '%s' and expected value '%s'",
		cfg.JsonQuery, condition, cfg.ExpectedValue)
}
