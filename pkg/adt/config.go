// Package adt provides a Go client for SAP ABAP Development Tools (ADT) REST API.
package adt

import (
	"crypto/tls"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// SessionType defines how the client manages server sessions.
type SessionType string

const (
	// SessionStateful maintains a server session via sap-contextid cookie.
	SessionStateful SessionType = "stateful"
	// SessionStateless does not persist sessions.
	SessionStateless SessionType = "stateless"
	// SessionKeep uses existing session if available, otherwise stateless.
	SessionKeep SessionType = "keep"
)

// Config holds the configuration for an ADT client connection.
type Config struct {
	// BaseURL is the SAP system URL (e.g., "https://vhcalnplci.dummy.nodomain:44300")
	BaseURL string
	// Username for SAP authentication
	Username string
	// Password for SAP authentication
	Password string
	// Client is the SAP client number (e.g., "001")
	Client string
	// Language for SAP session (e.g., "EN")
	Language string
	// InsecureSkipVerify disables TLS certificate verification
	InsecureSkipVerify bool
	// SessionType defines session management behavior
	SessionType SessionType
	// Timeout for HTTP requests
	Timeout time.Duration
	// Cookies for cookie-based authentication (alternative to basic auth)
	Cookies map[string]string
	// Verbose enables verbose logging
	Verbose bool
	// Safety defines protection parameters to prevent unintended modifications
	Safety SafetyConfig
	// Features controls optional feature detection and enablement
	Features FeatureConfig
	// TerminalID for debugger session (shared with SAP GUI for cross-tool debugging)
	TerminalID string

	// RFC connection settings (alternative to HTTP/BaseURL)
	ConnectionMode   string // "http" (default) or "rfc"
	AsHost           string // Direct app server hostname
	SysNr            string // System number (e.g., "00")
	MsHost           string // Message server host (load balancing)
	MsServ           string // Message server service/port
	R3Name           string // SAP system name
	Group            string // Logon group

	// JCo sidecar settings
	JcoProxyJar      string // Path to jco-proxy JAR
	JcoLibsDir       string // Path to JCo libraries directory
	JavaPath         string // Path to java binary (default: "java")
	RfcProxyPort     int    // Fixed sidecar port (0 = auto-assign)
	RfcMaxConcurrent int    // Max concurrent RFC calls (default: 5)
}

// Option is a functional option for configuring the ADT client.
type Option func(*Config)

// WithClient sets the SAP client number.
func WithClient(client string) Option {
	return func(c *Config) {
		c.Client = client
	}
}

// WithLanguage sets the SAP session language.
func WithLanguage(lang string) Option {
	return func(c *Config) {
		c.Language = lang
	}
}

// WithInsecureSkipVerify disables TLS certificate verification.
func WithInsecureSkipVerify() Option {
	return func(c *Config) {
		c.InsecureSkipVerify = true
	}
}

// WithSessionType sets the session management behavior.
func WithSessionType(st SessionType) Option {
	return func(c *Config) {
		c.SessionType = st
	}
}

// WithTimeout sets the HTTP request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.Timeout = d
	}
}

// WithCookies sets cookies for cookie-based authentication.
func WithCookies(cookies map[string]string) Option {
	return func(c *Config) {
		c.Cookies = cookies
	}
}

// WithVerbose enables verbose logging.
func WithVerbose() Option {
	return func(c *Config) {
		c.Verbose = true
	}
}

// WithSafety sets the safety configuration.
func WithSafety(safety SafetyConfig) Option {
	return func(c *Config) {
		c.Safety = safety
	}
}

// WithReadOnly enables read-only mode (blocks all write operations).
func WithReadOnly() Option {
	return func(c *Config) {
		c.Safety.ReadOnly = true
	}
}

// WithBlockFreeSQL blocks execution of arbitrary SQL queries.
func WithBlockFreeSQL() Option {
	return func(c *Config) {
		c.Safety.BlockFreeSQL = true
	}
}

// WithAllowedPackages restricts operations to specific packages.
func WithAllowedPackages(packages ...string) Option {
	return func(c *Config) {
		c.Safety.AllowedPackages = packages
	}
}

// WithEnableTransports enables transport management operations.
// By default, transport operations are disabled - this flag explicitly enables them.
func WithEnableTransports() Option {
	return func(c *Config) {
		c.Safety.EnableTransports = true
	}
}

// WithTransportReadOnly allows only read operations on transports (list, get).
// Create, release, delete operations will be blocked.
func WithTransportReadOnly() Option {
	return func(c *Config) {
		c.Safety.TransportReadOnly = true
	}
}

// WithAllowedTransports restricts transport operations to specific transports.
// Supports wildcards: "A4HK*" matches all transports starting with A4HK.
func WithAllowedTransports(transports ...string) Option {
	return func(c *Config) {
		c.Safety.AllowedTransports = transports
	}
}

// WithAllowTransportableEdits enables editing objects that require transport requests.
// By default, only local objects ($TMP, $* packages) can be edited.
// When enabled, users can provide transport parameters to EditSource/WriteSource.
// WARNING: This allows modifications to non-local objects that may affect production systems.
func WithAllowTransportableEdits() Option {
	return func(c *Config) {
		c.Safety.AllowTransportableEdits = true
	}
}

// HasBasicAuth returns true if username and password are configured.
func (c *Config) HasBasicAuth() bool {
	return c.Username != "" && c.Password != ""
}

// HasCookieAuth returns true if cookies are configured.
func (c *Config) HasCookieAuth() bool {
	return len(c.Cookies) > 0
}

// NewConfig creates a new Config with the given base URL, username, password,
// and optional configuration options.
func NewConfig(baseURL, username, password string, opts ...Option) *Config {
	cfg := &Config{
		BaseURL:     baseURL,
		Username:    username,
		Password:    password,
		Client:      "001",
		Language:    "EN",
		SessionType: SessionStateful,
		Timeout:     60 * time.Second,
		Safety:      UnrestrictedSafetyConfig(), // Default: no restrictions for backwards compatibility
		Features:    DefaultFeatureConfig(),     // Default: auto-detect all features
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return cfg
}

// WithFeatures sets the feature configuration.
func WithFeatures(features FeatureConfig) Option {
	return func(c *Config) {
		c.Features = features
	}
}

// WithTerminalID sets the debugger terminal ID.
// Use the same ID as SAP GUI to enable cross-tool breakpoint sharing.
// SAP GUI stores this in: Windows Registry HKCU\Software\SAP\ABAP Debugging\TerminalID
// or on Linux/Mac: ~/.SAP/ABAPDebugging/terminalId
func WithTerminalID(terminalID string) Option {
	return func(c *Config) {
		c.TerminalID = terminalID
	}
}

// IsRfcMode returns true if the connection mode is RFC.
func (c *Config) IsRfcMode() bool {
	return strings.EqualFold(c.ConnectionMode, "rfc")
}

// WithConnectionMode sets the connection mode ("http" or "rfc").
func WithConnectionMode(mode string) Option {
	return func(c *Config) {
		c.ConnectionMode = mode
	}
}

// WithAsHost sets the SAP application server hostname for direct RFC connection.
func WithAsHost(host string) Option {
	return func(c *Config) {
		c.AsHost = host
	}
}

// WithSysNr sets the SAP system number for direct RFC connection.
func WithSysNr(nr string) Option {
	return func(c *Config) {
		c.SysNr = nr
	}
}

// WithMsHost sets the message server host for load-balanced RFC connection.
func WithMsHost(host string) Option {
	return func(c *Config) {
		c.MsHost = host
	}
}

// WithMsServ sets the message server service/port for load-balanced RFC connection.
func WithMsServ(serv string) Option {
	return func(c *Config) {
		c.MsServ = serv
	}
}

// WithR3Name sets the SAP system name for load-balanced RFC connection.
func WithR3Name(name string) Option {
	return func(c *Config) {
		c.R3Name = name
	}
}

// WithGroup sets the logon group for load-balanced RFC connection.
func WithGroup(group string) Option {
	return func(c *Config) {
		c.Group = group
	}
}

// WithJcoProxyJar sets the path to the JCo proxy JAR file.
func WithJcoProxyJar(path string) Option {
	return func(c *Config) {
		c.JcoProxyJar = path
	}
}

// WithJcoLibsDir sets the path to the JCo libraries directory.
func WithJcoLibsDir(dir string) Option {
	return func(c *Config) {
		c.JcoLibsDir = dir
	}
}

// WithJavaPath sets the path to the Java binary.
func WithJavaPath(path string) Option {
	return func(c *Config) {
		c.JavaPath = path
	}
}

// WithRfcProxyPort sets a fixed port for the JCo sidecar (0 = auto-assign).
func WithRfcProxyPort(port int) Option {
	return func(c *Config) {
		c.RfcProxyPort = port
	}
}

// WithRfcMaxConcurrent sets the maximum number of concurrent RFC calls.
func WithRfcMaxConcurrent(n int) Option {
	return func(c *Config) {
		c.RfcMaxConcurrent = n
	}
}

// NewHTTPClient creates an http.Client configured for the given Config.
func (c *Config) NewHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)

	base := &http.Transport{
		Proxy: http.ProxyFromEnvironment, // Honor HTTP_PROXY/HTTPS_PROXY env vars
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: c.InsecureSkipVerify,
		},
	}

	// Wrap transport to fix SAP session cookies marked Secure over HTTP.
	// SAP often sets Secure flag even on HTTP connections, which causes
	// Go's cookie jar to drop them on subsequent requests.
	var transport http.RoundTripper = base
	if strings.HasPrefix(strings.ToLower(c.BaseURL), "http://") {
		transport = &stripSecureCookieTransport{base: base}
	}

	return &http.Client{
		Jar:       jar,
		Transport: transport,
		Timeout:   c.Timeout,
	}
}

// stripSecureCookieTransport wraps an http.RoundTripper and removes the Secure
// flag from Set-Cookie headers. This allows Go's cookie jar to persist SAP
// session cookies when connecting over plain HTTP.
type stripSecureCookieTransport struct {
	base http.RoundTripper
}

func (t *stripSecureCookieTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	// Strip Secure flag from Set-Cookie headers so the jar persists them over HTTP
	if cookies := resp.Header.Values("Set-Cookie"); len(cookies) > 0 {
		resp.Header.Del("Set-Cookie")
		for _, c := range cookies {
			resp.Header.Add("Set-Cookie", stripSecureFlag(c))
		}
	}
	return resp, err
}

// stripSecureFlag removes the Secure attribute from a Set-Cookie header value.
func stripSecureFlag(cookie string) string {
	parts := strings.Split(cookie, ";")
	filtered := parts[:0]
	for _, p := range parts {
		if !strings.EqualFold(strings.TrimSpace(p), "secure") {
			filtered = append(filtered, p)
		}
	}
	return strings.Join(filtered, ";")
}
