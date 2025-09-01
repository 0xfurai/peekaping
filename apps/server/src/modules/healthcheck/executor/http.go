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
	"peekaping/src/modules/certificate"
	"peekaping/src/modules/shared"
	"peekaping/src/utils"
	"peekaping/src/version"
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

	switch cfg.Encoding {
	case "json":
		if cfg.Body != "" {
			var js json.RawMessage
			if err := json.Unmarshal([]byte(cfg.Body), &js); err != nil {
				sl.ReportError(cfg.Body, "Body", "body", "json", "")
			}
		}
	case "form":
		if cfg.Body != "" {
			_, err := url.ParseQuery(cfg.Body)
			if err != nil {
				sl.ReportError(cfg.Body, "Body", "body", "form", "")
			}
		}
	case "xml":
		if cfg.Body != "" {
			if err := xml.Unmarshal([]byte(cfg.Body), new(interface{})); err != nil {
				sl.ReportError(cfg.Body, "Body", "body", "xml", "")
			}
		}
	case "text":
		// No validation needed
	}

	// Authentication validation
	switch cfg.AuthMethod {
	case "none":
		// No extra fields required
	case "basic":
		if cfg.BasicAuthUser == "" {
			sl.ReportError(cfg.BasicAuthUser, "BasicAuthUser", "basic_auth_user", "required_with_auth_basic", "")
		}
		if cfg.BasicAuthPass == "" {
			sl.ReportError(cfg.BasicAuthPass, "BasicAuthPass", "basic_auth_pass", "required_with_auth_basic", "")
		}
	case "ntlm":
		if cfg.BasicAuthUser == "" {
			sl.ReportError(cfg.BasicAuthUser, "BasicAuthUser", "basic_auth_user", "required_with_auth_ntlm", "")
		}
		if cfg.BasicAuthPass == "" {
			sl.ReportError(cfg.BasicAuthPass, "BasicAuthPass", "basic_auth_pass", "required_with_auth_ntlm", "")
		}
		if cfg.AuthDomain == "" {
			sl.ReportError(cfg.AuthDomain, "AuthDomain", "authDomain", "required_with_auth_ntlm", "")
		}
		if cfg.AuthWorkstation == "" {
			sl.ReportError(cfg.AuthWorkstation, "AuthWorkstation", "authWorkstation", "required_with_auth_ntlm", "")
		}
	case "oauth2-cc":
		if cfg.OauthAuthMethod != "client_secret_basic" && cfg.OauthAuthMethod != "client_secret_post" {
			sl.ReportError(cfg.OauthAuthMethod, "OauthAuthMethod", "oauth_auth_method", "oneof=client_secret_basic client_secret_post", "")
		}
		if cfg.OauthTokenUrl == "" {
			sl.ReportError(cfg.OauthTokenUrl, "OauthTokenUrl", "oauth_token_url", "required_with_auth_oauth2cc", "")
		} else {
			_, err := url.ParseRequestURI(cfg.OauthTokenUrl)
			if err != nil {
				sl.ReportError(cfg.OauthTokenUrl, "OauthTokenUrl", "oauth_token_url", "url", "")
			}
		}
		if cfg.OauthClientId == "" {
			sl.ReportError(cfg.OauthClientId, "OauthClientId", "oauth_client_id", "required_with_auth_oauth2cc", "")
		}
		if cfg.OauthClientSecret == "" {
			sl.ReportError(cfg.OauthClientSecret, "OauthClientSecret", "oauth_client_secret", "required_with_auth_oauth2cc", "")
		}
		// OauthScopes is optional
	case "mtls":
		if cfg.TlsCert == "" {
			sl.ReportError(cfg.TlsCert, "TlsCert", "tlsCert", "required_with_auth_mtls", "")
		}
		if cfg.TlsKey == "" {
			sl.ReportError(cfg.TlsKey, "TlsKey", "tlsKey", "required_with_auth_mtls", "")
		}
		if cfg.TlsCa == "" {
			sl.ReportError(cfg.TlsCa, "TlsCa", "tlsCa", "required_with_auth_mtls", "")
		}
	}
}

type HTTPConfig struct {
	Url string `json:"url" validate:"required,url"`

	Method              string   `json:"method" validate:"required,oneof=GET POST PUT DELETE PATCH HEAD OPTIONS"`
	Headers             string   `json:"headers" validate:"omitempty,json"`
	Encoding            string   `json:"encoding" validate:"required,oneof=json form xml text"`
	Body                string   `json:"body" validate:"omitempty"`
	AcceptedStatusCodes []string `json:"accepted_statuscodes" validate:"required,dive,oneof=2XX 3XX 4XX 5XX"`
	MaxRedirects        int      `json:"max_redirects" validate:"omitempty,min=0"`
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
	client *http.Client
	logger *zap.SugaredLogger
}

// TLSInterceptor is a custom RoundTripper that captures TLS certificate information
type TLSInterceptor struct {
	transport http.RoundTripper
	tlsInfo   *certificate.TLSInfo
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

func (t *TLSInterceptor) extractTLSInfo(tlsState *tls.ConnectionState) *certificate.TLSInfo {
	if len(tlsState.PeerCertificates) == 0 {
		return &certificate.TLSInfo{Valid: false}
	}

	// Get the server certificate (first in the chain)
	serverCert := tlsState.PeerCertificates[0]

	// Check if the certificate chain is verified
	verified := len(tlsState.VerifiedChains) > 0

	return certificate.ParseCertificateChain(serverCert, verified)
}

func (t *TLSInterceptor) GetTLSInfo() *certificate.TLSInfo {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.tlsInfo
}

func (t *TLSInterceptor) Reset() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.tlsInfo = nil
}

func NewHTTPExecutor(logger *zap.SugaredLogger) *HTTPExecutor {
	utils.Validate.RegisterStructValidation(HTTPConfigStructLevelValidation, HTTPConfig{})

	return &HTTPExecutor{
		client: &http.Client{},
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

// Helper to check if status code matches accepted patterns
func isStatusAccepted(statusCode int, accepted []string) bool {
	for _, pattern := range accepted {
		switch pattern {
		case "2XX":
			if statusCode >= 200 && statusCode < 300 {
				return true
			}
		case "3XX":
			if statusCode >= 300 && statusCode < 400 {
				return true
			}
		case "4XX":
			if statusCode >= 400 && statusCode < 500 {
				return true
			}
		case "5XX":
			if statusCode >= 500 && statusCode < 600 {
				return true
			}
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
			var responseJson, expectedJson interface{}

			// Try to parse both as JSON first
			responseIsJson := json.Unmarshal([]byte(responseBody), &responseJson) == nil
			expectedIsJson := json.Unmarshal([]byte(expectedValue), &expectedJson) == nil

			// If both are valid JSON, do JSON comparison
			if responseIsJson && expectedIsJson {
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
				actualValue := string(responseJsonBytes)
				expectedValueNormalized := string(expectedJsonBytes)

				// Default condition is equality
				if condition == "" {
					condition = "=="
				}

				switch condition {
				case "==":
					return actualValue == expectedValueNormalized, nil
				case "!=":
					return actualValue != expectedValueNormalized, nil
				default:
					return false, fmt.Errorf("unsupported condition '%s' for full JSON comparison: only '==' and '!=' are supported", condition)
				}
			}

			// If one or both are not valid JSON, fall back to string comparison
			// But only allow == and != conditions for non-JSON comparison
			if condition == "" {
				condition = "=="
			}

			switch condition {
			case "==":
				return responseBody == expectedValue, nil
			case "!=":
				return responseBody != expectedValue, nil
			default:
				return false, fmt.Errorf("unsupported condition '%s' for non-JSON comparison: only '==' and '!=' are supported", condition)
			}
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

	actualValue := result.String()

	// Default condition is equality
	if condition == "" {
		condition = "=="
	}

	switch condition {
	case "==":
		return actualValue == expectedValue, nil
	case "!=":
		return actualValue != expectedValue, nil
	case ">":
		actualFloat, err1 := strconv.ParseFloat(actualValue, 64)
		expectedFloat, err2 := strconv.ParseFloat(expectedValue, 64)
		if err1 != nil || err2 != nil {
			return strings.Compare(actualValue, expectedValue) > 0, nil
		}
		return actualFloat > expectedFloat, nil
	case "<":
		actualFloat, err1 := strconv.ParseFloat(actualValue, 64)
		expectedFloat, err2 := strconv.ParseFloat(expectedValue, 64)
		if err1 != nil || err2 != nil {
			return strings.Compare(actualValue, expectedValue) < 0, nil
		}
		return actualFloat < expectedFloat, nil
	case ">=":
		actualFloat, err1 := strconv.ParseFloat(actualValue, 64)
		expectedFloat, err2 := strconv.ParseFloat(expectedValue, 64)
		if err1 != nil || err2 != nil {
			return strings.Compare(actualValue, expectedValue) >= 0, nil
		}
		return actualFloat >= expectedFloat, nil
	case "<=":
		actualFloat, err1 := strconv.ParseFloat(actualValue, 64)
		expectedFloat, err2 := strconv.ParseFloat(expectedValue, 64)
		if err1 != nil || err2 != nil {
			return strings.Compare(actualValue, expectedValue) <= 0, nil
		}
		return actualFloat <= expectedFloat, nil
	default:
		return false, fmt.Errorf("unsupported condition: %s", condition)
	}
}

func buildProxyTransport(base *http.Transport, proxyModel *Proxy) http.RoundTripper {
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
		proxyURL := &url.URL{
			Scheme: protocol,
			Host:   fmt.Sprintf("%s:%d", proxyModel.Host, proxyModel.Port),
		}
		if proxyModel.Auth && proxyModel.Username != "" && proxyModel.Password != "" {
			proxyURL.User = url.UserPassword(proxyModel.Username, proxyModel.Password)
		}
		base.Proxy = http.ProxyURL(proxyURL)
		return base
	case "socks", "socks5", "socks5h", "socks4":
		var auth *proxy.Auth
		if proxyModel.Auth && proxyModel.Username != "" && proxyModel.Password != "" {
			auth = &proxy.Auth{
				User:     proxyModel.Username,
				Password: proxyModel.Password,
			}
		}
		address := fmt.Sprintf("%s:%d", proxyModel.Host, proxyModel.Port)
		dialer, err := proxy.SOCKS5("tcp", address, auth, proxy.Direct)
		if err != nil {
			// fallback to default transport if dialer fails
			return base
		}
		base.DialContext = func(ctx context.Context, network, addr string) (conn net.Conn, e error) {
			return dialer.Dial(network, addr)
		}
		base.Proxy = nil // No HTTP proxy
		return base
	default:
		return base
	}
}

func setDefaultHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "peekaping/"+version.Version)
	req.Header.Set("Accept", "*/*")
}

func (h *HTTPExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := h.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*HTTPConfig)

	h.logger.Debugf("execute http cfg: %+v", cfg)

	var bodyReader io.Reader
	if cfg.Body != "" {
		bodyReader = bytes.NewReader([]byte(cfg.Body))
	}

	req, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.Url, bodyReader)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	setDefaultHeaders(req)

	if cfg.Headers != "" {
		headersMap := make(map[string]string)
		err := json.Unmarshal([]byte(cfg.Headers), &headersMap)
		if err != nil {
			return DownResult(fmt.Errorf("invalid headers json: %w", err), time.Now().UTC(), time.Now().UTC())
		}
		for k, v := range headersMap {
			req.Header.Set(k, v)
		}
	}

	// Determine effective max redirects value
	effectiveMaxRedirects := cfg.MaxRedirects

	checkRedirect := func(req *http.Request, via []*http.Request) error {
		h.logger.Debugf("checkRedirect: %d redirects followed, max allowed: %d", len(via), effectiveMaxRedirects)
		if effectiveMaxRedirects == 0 {
			return fmt.Errorf("redirects disabled: max_redirects set to 0")
		}
		if len(via) > effectiveMaxRedirects {
			return fmt.Errorf("too many redirects: followed %d redirects, maximum allowed is %d", len(via), effectiveMaxRedirects)
		}
		return nil
	}

	switch cfg.Encoding {
	case "json":
		req.Header.Set("Content-Type", "application/json")
	case "form":
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	case "xml":
		req.Header.Set("Content-Type", "application/xml")
	case "text":
		req.Header.Set("Content-Type", "text/plain")
	}

	// --- PROXY LOGIC ---

	// Default transport with proxy if needed
	baseTransport := &http.Transport{}

	// Configure TLS settings if needed
	if cfg.IgnoreTlsErrors {
		if baseTransport.TLSClientConfig == nil {
			baseTransport.TLSClientConfig = &tls.Config{}
		}
		baseTransport.TLSClientConfig.InsecureSkipVerify = true
	}

	transport := buildProxyTransport(baseTransport, proxyModel)

	// Create TLS interceptor to capture certificate information
	tlsInterceptor := NewTLSInterceptor(transport)

	// Keep track of the active TLS interceptor for error handling
	var activeTLSInterceptor *TLSInterceptor = tlsInterceptor

	// Set timeout from monitor configuration
	timeout := time.Duration(m.Timeout) * time.Second

	// --- AUTHENTICATION LOGIC ---
	switch cfg.AuthMethod {
	case "basic":
		req.SetBasicAuth(cfg.BasicAuthUser, cfg.BasicAuthPass)
	case "ntlm":
		// NTLM authentication using github.com/Azure/go-ntlmssp
		ntlmTransport := ntlmssp.Negotiator{
			RoundTripper: tlsInterceptor,
		}
		h.client = &http.Client{
			Transport:     &ntlmTransport,
			Timeout:       time.Duration(m.Timeout) * time.Second,
			CheckRedirect: checkRedirect,
		}

		if cfg.AuthDomain != "" {
			req.SetBasicAuth(cfg.AuthDomain+"\\"+cfg.BasicAuthUser, cfg.BasicAuthPass)
		} else {
			req.SetBasicAuth(cfg.BasicAuthUser, cfg.BasicAuthPass)
		}
	case "oauth2-cc":
		form := url.Values{}
		form.Set("grant_type", "client_credentials")
		if cfg.OauthScopes != "" {
			form.Set("scope", cfg.OauthScopes)
		}
		form.Set("client_id", cfg.OauthClientId)
		form.Set("client_secret", cfg.OauthClientSecret)

		tokenReq, err := http.NewRequestWithContext(ctx, "POST", cfg.OauthTokenUrl, strings.NewReader(form.Encode()))
		if err != nil {
			return DownResult(fmt.Errorf("failed to create oauth2 token request: %w", err), time.Now().UTC(), time.Now().UTC())
		}
		setDefaultHeaders(tokenReq)

		tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cfg.OauthAuthMethod == "client_secret_basic" {
			basic := base64.StdEncoding.EncodeToString([]byte(cfg.OauthClientId + ":" + cfg.OauthClientSecret))
			tokenReq.Header.Set("Authorization", "Basic "+basic)
		}

		tokenResp, err := http.DefaultClient.Do(tokenReq)
		if err != nil {
			return DownResult(fmt.Errorf("failed to get oauth2 token: %w", err), time.Now().UTC(), time.Now().UTC())
		}
		defer tokenResp.Body.Close()
		if tokenResp.StatusCode < 200 || tokenResp.StatusCode >= 300 {
			return DownResult(fmt.Errorf("oauth2 token endpoint returned status: %d", tokenResp.StatusCode), time.Now().UTC(), time.Now().UTC())
		}
		var tokenData struct {
			AccessToken string `json:"access_token"`
		}
		err = json.NewDecoder(tokenResp.Body).Decode(&tokenData)
		if err != nil || tokenData.AccessToken == "" {
			return DownResult(fmt.Errorf("failed to parse oauth2 token response: %w", err), time.Now().UTC(), time.Now().UTC())
		}
		req.Header.Set("Authorization", "Bearer "+tokenData.AccessToken)
	case "mtls":
		cert, err := tls.X509KeyPair([]byte(cfg.TlsCert), []byte(cfg.TlsKey))
		if err != nil {
			return DownResult(fmt.Errorf("invalid mTLS cert/key: %w", err), time.Now().UTC(), time.Now().UTC())
		}
		caCertPool := x509.NewCertPool()
		if ok := caCertPool.AppendCertsFromPEM([]byte(cfg.TlsCa)); !ok {
			return DownResult(fmt.Errorf("invalid mTLS CA cert"), time.Now().UTC(), time.Now().UTC())
		}
		mtlsTransport := &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates:       []tls.Certificate{cert},
				RootCAs:            caCertPool,
				InsecureSkipVerify: cfg.IgnoreTlsErrors,
			},
		}
		mtlsTransportWithProxy := buildProxyTransport(mtlsTransport, proxyModel)
		mtlsTLSInterceptor := NewTLSInterceptor(mtlsTransportWithProxy)
		activeTLSInterceptor = mtlsTLSInterceptor // Update the active interceptor for mTLS
		h.client = &http.Client{
			Transport:     mtlsTLSInterceptor,
			Timeout:       time.Duration(m.Timeout) * time.Second,
			CheckRedirect: checkRedirect,
		}
	}

	if cfg.AuthMethod != "mtls" && cfg.AuthMethod != "ntlm" {
		h.client = &http.Client{
			Timeout:       timeout,
			CheckRedirect: checkRedirect,
			Transport:     tlsInterceptor,
		}
	}

	// Set user agent and accept headers

	startTime := time.Now().UTC()
	resp, err := h.client.Do(req)
	endTime := time.Now().UTC()

	if err != nil {
		h.logger.Infof("HTTP request failed: %s, %s", m.Name, err.Error())
		result := DownResult(err, startTime, endTime)
		// Try to get TLS info even on error for HTTPS requests
		if strings.HasPrefix(cfg.Url, "https://") && activeTLSInterceptor != nil {
			result.TLSInfo = activeTLSInterceptor.GetTLSInfo()
		}
		return result
	}
	defer resp.Body.Close()

	h.logger.Infof("HTTP response status: %s, %d", m.Name, resp.StatusCode)

	// Extract TLS information if available
	var tlsInfo *certificate.TLSInfo
	if strings.HasPrefix(cfg.Url, "https://") && activeTLSInterceptor != nil {
		tlsInfo = activeTLSInterceptor.GetTLSInfo()
	}

	if !isStatusAccepted(resp.StatusCode, cfg.AcceptedStatusCodes) {
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("HTTP request failed with status: %d", resp.StatusCode),
			StartTime: startTime,
			EndTime:   endTime,
			TLSInfo:   tlsInfo,
		}
	}

	// Read response body for content validation
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("Failed to read response body: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
			TLSInfo:   tlsInfo,
		}
	}
	var responseBody = string(bodyBytes)
	h.logger.Debugf("Response body length: %d", len(responseBody))

	// Check keyword if specified
	if cfg.Keyword != "" {
		if !checkKeyword(responseBody, cfg.Keyword, cfg.InvertKeyword) {
			var message string
			if cfg.InvertKeyword {
				message = fmt.Sprintf("Keyword check failed: keyword '%s' found in response (expected absent)", cfg.Keyword)
			} else {
				message = fmt.Sprintf("Keyword check failed: keyword '%s' not found in response", cfg.Keyword)
			}
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   message,
				StartTime: startTime,
				EndTime:   endTime,
				TLSInfo:   tlsInfo,
			}
		}
	}

	// Check JSON query if specified
	if m.Type == "http-json-query" {
		isValid, err := checkJsonQuery(responseBody, cfg.JsonQuery, cfg.JsonCondition, cfg.ExpectedValue)
		if err != nil {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("JSON query validation error: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
				TLSInfo:   tlsInfo,
			}
		}
		if !isValid {
			condition := cfg.JsonCondition
			if condition == "" {
				condition = "=="
			}
			message := fmt.Sprintf("JSON query validation failed: query '%s' with condition '%s' and expected value '%s'",
				cfg.JsonQuery, condition, cfg.ExpectedValue)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   message,
				StartTime: startTime,
				EndTime:   endTime,
				TLSInfo:   tlsInfo,
			}
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
