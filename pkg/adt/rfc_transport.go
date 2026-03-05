package adt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ProxyRequest is the JSON payload sent to the Java sidecar's /rfc-proxy endpoint.
type ProxyRequest struct {
	Method  string            `json:"method"`
	URI     string            `json:"uri"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// ProxyResponse is the JSON payload returned by the Java sidecar's /rfc-proxy endpoint.
type ProxyResponse struct {
	StatusCode   int               `json:"statusCode"`
	ReasonPhrase string            `json:"reasonPhrase"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`
}

// RfcTransport implements Requester by proxying HTTP requests through a Java JCo sidecar.
// The sidecar translates HTTP→RFC via SAP's SADT_REST_RFC_ENDPOINT function module.
type RfcTransport struct {
	sidecarURL string
	httpClient *http.Client
	config     *Config

	// Session management — forward sap-contextid from proxy responses
	sessionCookie string
	sessionMu     sync.RWMutex

	// CSRF token from proxy responses
	csrfToken string
	csrfMu    sync.RWMutex

	// Concurrency control
	semaphore chan struct{}
}

// NewRfcTransport creates a new RfcTransport that proxies requests through the sidecar.
func NewRfcTransport(sidecarURL string, cfg *Config, maxConcurrent int) *RfcTransport {
	if maxConcurrent <= 0 {
		maxConcurrent = 5
	}
	return &RfcTransport{
		sidecarURL: strings.TrimSuffix(sidecarURL, "/"),
		httpClient: &http.Client{}, // No hard timeout — context deadline controls per-request timeouts
		config:     cfg,
		semaphore:  make(chan struct{}, maxConcurrent),
	}
}

// Request implements Requester by converting the ADT request into a ProxyRequest JSON,
// POSTing it to the sidecar's /rfc-proxy endpoint, and parsing the ProxyResponse back.
func (r *RfcTransport) Request(ctx context.Context, path string, opts *RequestOptions) (*Response, error) {
	if opts == nil {
		opts = &RequestOptions{}
	}
	if opts.Method == "" {
		opts.Method = http.MethodGet
	}

	// Acquire semaphore slot
	select {
	case r.semaphore <- struct{}{}:
		defer func() { <-r.semaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Build the full URI with query params
	uri, err := r.buildURI(path, opts.Query)
	if err != nil {
		return nil, fmt.Errorf("building URI: %w", err)
	}

	// Build proxy request headers
	headers := make(map[string]string)

	// Accept header
	accept := opts.Accept
	if accept == "" {
		accept = "*/*"
	}
	headers["Accept"] = accept

	// Content-Type for requests with body
	if opts.Body != nil {
		ct := opts.ContentType
		if ct == "" {
			ct = "application/xml"
		}
		headers["Content-Type"] = ct
	}

	// Custom headers from opts
	for k, v := range opts.Headers {
		headers[k] = v
	}

	// Session type header
	switch r.config.SessionType {
	case SessionStateful:
		headers["X-sap-adt-sessiontype"] = "stateful"
	case SessionStateless:
		headers["X-sap-adt-sessiontype"] = "stateless"
	}

	// Forward session cookie
	if cookie := r.getSessionCookie(); cookie != "" {
		headers["Cookie"] = "sap-contextid=" + cookie
	}

	// Forward CSRF token for modifying requests
	if isModifyingMethod(opts.Method) {
		if token := r.getCSRFToken(); token != "" {
			headers["X-CSRF-Token"] = token
		}
	}

	// Build proxy request
	proxyReq := ProxyRequest{
		Method:  opts.Method,
		URI:     uri,
		Headers: headers,
	}
	if opts.Body != nil {
		proxyReq.Body = string(opts.Body)
	}

	// Marshal and send to sidecar
	proxyResp, err := r.sendToSidecar(ctx, &proxyReq)
	if err != nil {
		return nil, err
	}

	// Extract session cookie from proxy response headers
	if cookieHeader, ok := proxyResp.Headers["Set-Cookie"]; ok {
		if id := extractContextID(cookieHeader); id != "" {
			r.setSessionCookie(id)
		}
	}
	// Also check lowercase (sidecar may normalize)
	if cookieHeader, ok := proxyResp.Headers["set-cookie"]; ok {
		if id := extractContextID(cookieHeader); id != "" {
			r.setSessionCookie(id)
		}
	}

	// Extract CSRF token from proxy response
	if token, ok := proxyResp.Headers["X-CSRF-Token"]; ok && token != "" && token != "Required" {
		r.setCSRFToken(token)
	}
	if token, ok := proxyResp.Headers["x-csrf-token"]; ok && token != "" && token != "Required" {
		r.setCSRFToken(token)
	}

	// Convert proxy response to Response
	respHeaders := http.Header{}
	for k, v := range proxyResp.Headers {
		respHeaders.Set(k, v)
	}

	// Check for error status codes
	if proxyResp.StatusCode >= 400 {
		return nil, &APIError{
			StatusCode: proxyResp.StatusCode,
			Message:    proxyResp.Body,
			Path:       path,
		}
	}

	return &Response{
		StatusCode: proxyResp.StatusCode,
		Headers:    respHeaders,
		Body:       []byte(proxyResp.Body),
	}, nil
}

// sendToSidecar POSTs a ProxyRequest to the sidecar and returns the ProxyResponse.
func (r *RfcTransport) sendToSidecar(ctx context.Context, proxyReq *ProxyRequest) (*ProxyResponse, error) {
	body, err := json.Marshal(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling proxy request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.sidecarURL+"/rfc-proxy", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating sidecar request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sidecar request failed (is the sidecar running at %s?): %w", r.sidecarURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading sidecar response: %w", err)
	}

	// The sidecar itself should always return 200; the SAP status is inside the JSON
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var proxyResp ProxyResponse
	if err := json.Unmarshal(respBody, &proxyResp); err != nil {
		return nil, fmt.Errorf("parsing sidecar response: %w", err)
	}

	return &proxyResp, nil
}

// buildURI constructs the URI path with query parameters for the proxy request.
func (r *RfcTransport) buildURI(path string, query url.Values) (string, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	u, err := url.Parse(path)
	if err != nil {
		return "", err
	}

	// Merge query parameters
	q := u.Query()
	if r.config.Client != "" {
		q.Set("sap-client", r.config.Client)
	}
	if r.config.Language != "" {
		q.Set("sap-language", r.config.Language)
	}
	for k, v := range query {
		for _, val := range v {
			q.Add(k, val)
		}
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// extractContextID extracts sap-contextid value from a Set-Cookie header string.
func extractContextID(cookieHeader string) string {
	for _, part := range strings.Split(cookieHeader, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "sap-contextid=") {
			return strings.TrimPrefix(part, "sap-contextid=")
		}
	}
	// Also handle multiple cookies separated by comma
	for _, cookie := range strings.Split(cookieHeader, ",") {
		for _, part := range strings.Split(cookie, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "sap-contextid=") {
				return strings.TrimPrefix(part, "sap-contextid=")
			}
		}
	}
	return ""
}

// Session cookie accessors
func (r *RfcTransport) getSessionCookie() string {
	r.sessionMu.RLock()
	defer r.sessionMu.RUnlock()
	return r.sessionCookie
}

func (r *RfcTransport) setSessionCookie(cookie string) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	r.sessionCookie = cookie
}

// CSRF token accessors
func (r *RfcTransport) getCSRFToken() string {
	r.csrfMu.RLock()
	defer r.csrfMu.RUnlock()
	return r.csrfToken
}

func (r *RfcTransport) setCSRFToken(token string) {
	r.csrfMu.Lock()
	defer r.csrfMu.Unlock()
	r.csrfToken = token
}

// SetTimeout updates the HTTP client timeout for sidecar requests.
func (r *RfcTransport) SetTimeout(d time.Duration) {
	r.httpClient.Timeout = d
}
