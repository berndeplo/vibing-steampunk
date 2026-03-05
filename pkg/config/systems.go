// Package config provides system configuration management for vsp CLI.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemConfig represents a SAP system configuration.
type SystemConfig struct {
	URL      string `json:"url"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"` // Not recommended, use env var
	Client   string `json:"client,omitempty"`
	Language string `json:"language,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`

	// Cookie authentication (alternative to user/password)
	CookieFile   string `json:"cookie_file,omitempty"`   // Path to Netscape-format cookie file
	CookieString string `json:"cookie_string,omitempty"` // Inline cookie string

	// RFC connection settings (alternative to URL-based HTTP)
	ConnectionMode string `json:"connection_mode,omitempty"` // "http" (default) or "rfc"
	AsHost         string `json:"ashost,omitempty"`
	SysNr          string `json:"sysnr,omitempty"`
	MsHost         string `json:"mshost,omitempty"`
	MsServ         string `json:"msserv,omitempty"`
	R3Name         string `json:"r3name,omitempty"`
	Group          string `json:"group,omitempty"`
	JcoLibsDir     string `json:"jco_libs_dir,omitempty"`
	JcoProxyJar    string `json:"jco_proxy_jar,omitempty"`
	JavaPath       string `json:"java_path,omitempty"`

	// Optional safety settings per system
	ReadOnly        bool     `json:"read_only,omitempty"`
	AllowedPackages []string `json:"allowed_packages,omitempty"`
}

// SystemsConfig is the root configuration containing all systems.
type SystemsConfig struct {
	Systems map[string]SystemConfig `json:"systems"`
	Default string                  `json:"default,omitempty"`

	// Tools configuration - granular tool visibility control
	// Key: tool name, Value: true=enabled, false=disabled
	// Tools not listed are enabled by default
	Tools map[string]bool `json:"tools,omitempty"`
}

// ConfigPaths returns the list of paths to search for systems config.
func ConfigPaths() []string {
	paths := []string{
		".vsp.json",                   // Current directory (preferred)
		".vsp/systems.json",           // Current directory .vsp folder
	}

	// Add home directory paths
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".vsp.json"),
			filepath.Join(home, ".vsp", "systems.json"),
		)
	}

	return paths
}

// LoadSystems loads systems configuration from the first found config file.
func LoadSystems() (*SystemsConfig, string, error) {
	for _, path := range ConfigPaths() {
		if _, err := os.Stat(path); err == nil {
			cfg, err := LoadSystemsFromFile(path)
			if err != nil {
				return nil, path, err
			}
			return cfg, path, nil
		}
	}
	return nil, "", nil // No config file found (not an error)
}

// LoadSystemsFromFile loads systems configuration from a specific file.
func LoadSystemsFromFile(path string) (*SystemsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg SystemsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// GetSystem retrieves a system configuration by name, resolving password from env.
func (c *SystemsConfig) GetSystem(name string) (*SystemConfig, error) {
	sys, ok := c.Systems[name]
	if !ok {
		// List available systems in error
		available := make([]string, 0, len(c.Systems))
		for k := range c.Systems {
			available = append(available, k)
		}
		return nil, fmt.Errorf("system '%s' not found. Available: %s", name, strings.Join(available, ", "))
	}

	// Resolve password from environment variable if not set
	if sys.Password == "" {
		// Try VSP_<SYSTEM>_PASSWORD (e.g., VSP_A4H_PASSWORD)
		envKey := fmt.Sprintf("VSP_%s_PASSWORD", strings.ToUpper(name))
		if pwd := os.Getenv(envKey); pwd != "" {
			sys.Password = pwd
		}
	}

	// Fallback: resolve password from .mcp.json env block
	if sys.Password == "" {
		envKey := fmt.Sprintf("VSP_%s_PASSWORD", strings.ToUpper(name))
		if pwd := loadMcpEnvVar(envKey); pwd != "" {
			sys.Password = pwd
		}
	}

	// Apply defaults
	if sys.Client == "" {
		sys.Client = "001"
	}
	if sys.Language == "" {
		sys.Language = "EN"
	}

	return &sys, nil
}

// mcpConfig represents the structure of .mcp.json for env var extraction.
type mcpConfig struct {
	McpServers map[string]struct {
		Env map[string]string `json:"env"`
	} `json:"mcpServers"`
}

// loadMcpEnvVar searches .mcp.json env blocks for a given variable name.
func loadMcpEnvVar(key string) string {
	for _, path := range []string{".mcp.json"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg mcpConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		for _, server := range cfg.McpServers {
			if val, ok := server.Env[key]; ok {
				return val
			}
		}
	}
	return ""
}

// ListSystems returns a list of configured system names.
func (c *SystemsConfig) ListSystems() []string {
	systems := make([]string, 0, len(c.Systems))
	for name := range c.Systems {
		systems = append(systems, name)
	}
	return systems
}

// ExampleConfig returns an example configuration for documentation.
func ExampleConfig() string {
	example := SystemsConfig{
		Default: "dev",
		Systems: map[string]SystemConfig{
			"dev": {
				URL:    "http://dev.example.com:50000",
				User:   "DEVELOPER",
				Client: "001",
			},
			"a4h": {
				URL:      "http://a4h.local:50000",
				User:     "ADMIN",
				Client:   "001",
				Insecure: true,
			},
			"prod": {
				URL:             "https://prod.example.com:44300",
				User:            "READONLY_USER",
				Client:          "100",
				ReadOnly:        true,
				AllowedPackages: []string{"Z*", "Y*"},
			},
			"rfc-direct": {
				ConnectionMode: "rfc",
				AsHost:         "sap-app.example.com",
				SysNr:          "00",
				User:           "RFC_USER",
				Client:         "001",
				JcoProxyJar:    "/opt/vsp/jco-proxy.jar",
				JcoLibsDir:     "/opt/sap/jco",
			},
		},
	}

	data, _ := json.MarshalIndent(example, "", "  ")
	return string(data)
}

// IsToolEnabled checks if a tool is enabled in the configuration.
// Tools not explicitly listed are enabled by default.
func (c *SystemsConfig) IsToolEnabled(toolName string) bool {
	if c.Tools == nil {
		return true
	}
	enabled, exists := c.Tools[toolName]
	if !exists {
		return true // Default: enabled
	}
	return enabled
}

// GetDisabledTools returns a list of explicitly disabled tools.
func (c *SystemsConfig) GetDisabledTools() []string {
	var disabled []string
	for name, enabled := range c.Tools {
		if !enabled {
			disabled = append(disabled, name)
		}
	}
	return disabled
}

// SetToolEnabled sets the enabled state for a tool.
func (c *SystemsConfig) SetToolEnabled(toolName string, enabled bool) {
	if c.Tools == nil {
		c.Tools = make(map[string]bool)
	}
	c.Tools[toolName] = enabled
}

// SaveToFile saves the configuration to a file.
func (c *SystemsConfig) SaveToFile(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}

// DefaultDisabledTools returns the list of tools that should be disabled by default.
// These are experimental or non-working tools.
func DefaultDisabledTools() []string {
	return []string{
		// AMDP/HANA Debugger - session management issues
		"AMDPDebuggerStart", "AMDPDebuggerResume", "AMDPDebuggerStop",
		"AMDPDebuggerStep", "AMDPGetVariables", "AMDPSetBreakpoint", "AMDPGetBreakpoints",
		// ABAP Debugger - requires ZADT_VSP WebSocket, HTTP unreliable
		"DebuggerListen", "DebuggerAttach", "DebuggerDetach",
		"DebuggerStep", "DebuggerGetStack", "DebuggerGetVariables",
		// Breakpoints - requires ZADT_VSP WebSocket
		"SetBreakpoint", "GetBreakpoints", "DeleteBreakpoint",
		// UI5 write operations - need alternate API
		"UI5CreateApp", "UI5DeleteApp", "UI5DeleteFile", "UI5UploadFile",
	}
}
