// vsp is an MCP server providing ABAP Development Tools (ADT) functionality.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/oisee/vibing-steampunk/internal/mcp"
	"github.com/oisee/vibing-steampunk/pkg/adt"
	"github.com/oisee/vibing-steampunk/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// Version information (set by build flags)
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

var cfg = &mcp.Config{}

var rootCmd = &cobra.Command{
	Use:   "vsp",
	Short: "ABAP Development Tools for AI agents and DevOps",
	Long: `vsp — ABAP Development Tools for AI agents and DevOps.

Single binary, 9 platforms, no dependencies. Download from GitHub releases,
point your MCP config at it, done.

Two modes of operation:

  MCP Server (default)  Connects Claude, Gemini CLI, Copilot, Codex, Qwen Code,
                        and other MCP-compatible agents to SAP systems.
                        81 tools in focused mode, 122 in expert mode.

  CLI Mode              Direct terminal access: search, source, export, debug.
                        Multi-system profiles. Useful for scripts and pipelines.

Quick start:
  # 1. MCP server (reads .env or SAP_* env vars)
  vsp --url https://host:44300 --user dev --password secret

  # 2. CLI mode with saved system profile
  vsp -s dev search "ZCL_ORDER*"
  vsp -s dev source CLAS ZCL_ORDER_PROCESSING
  vsp -s dev export '$ZPACKAGE' -o backup.zip

  # 3. Enterprise safety (hand to AI without fear)
  vsp --read-only                                    # no writes at all
  vsp --allowed-packages 'Z*,$TMP' --block-free-sql  # sandbox AI to custom code
  vsp --disallowed-ops CDUA                           # block create/delete/update/activate

Configuration files:
  .env          Default SAP connection (MCP server mode). SAP_URL, SAP_USER, etc.
  .vsp.json     Multi-system profiles for CLI mode (vsp -s dev, vsp -s prod).
  .mcp.json     MCP server entries for Claude Desktop / other MCP clients.

  vsp config init       Generate example files (.env.example, .vsp.json.example, .mcp.json.example)
  vsp config show       Display effective configuration
  vsp config mcp-to-vsp Import systems from .mcp.json into .vsp.json
  vsp config vsp-to-mcp Export .vsp.json systems to .mcp.json format
  vsp config tools      Manage per-tool visibility in .vsp.json

Configuration priority: CLI flags > env vars > .env file > defaults
Ready-to-use configs for 8 AI agents: docs/cli-agents/`,
	Version: fmt.Sprintf("%s (commit: %s, built: %s)", Version, Commit, BuildDate),
	RunE:    runServer,
}

func init() {
	// Load .env file if it exists
	godotenv.Load()

	// Service URL
	rootCmd.Flags().StringVar(&cfg.BaseURL, "url", "", "SAP system URL (e.g., https://host:44300)")
	rootCmd.Flags().StringVar(&cfg.BaseURL, "service", "", "SAP system URL (alias for --url)")

	// Authentication flags
	rootCmd.Flags().StringVarP(&cfg.Username, "user", "u", "", "SAP username")
	rootCmd.Flags().StringVarP(&cfg.Password, "password", "p", "", "SAP password")
	rootCmd.Flags().StringVar(&cfg.Password, "pass", "", "SAP password (alias for --password)")

	// SAP connection options
	rootCmd.Flags().StringVar(&cfg.Client, "client", "001", "SAP client number")
	rootCmd.Flags().StringVar(&cfg.Language, "language", "EN", "SAP language")
	rootCmd.Flags().BoolVar(&cfg.InsecureSkipVerify, "insecure", false, "Skip TLS certificate verification")

	// Cookie authentication
	rootCmd.Flags().String("cookie-file", "", "Path to cookie file in Netscape format")
	rootCmd.Flags().String("cookie-string", "", "Cookie string (key1=val1; key2=val2)")

	// Safety options
	rootCmd.Flags().BoolVar(&cfg.ReadOnly, "read-only", false, "Block all write operations (create, update, delete, activate)")
	rootCmd.Flags().BoolVar(&cfg.BlockFreeSQL, "block-free-sql", false, "Block execution of arbitrary SQL queries via RunQuery")
	rootCmd.Flags().StringVar(&cfg.AllowedOps, "allowed-ops", "", "Whitelist of allowed operation types (e.g., \"RSQ\" for Read, Search, Query only)")
	rootCmd.Flags().StringVar(&cfg.DisallowedOps, "disallowed-ops", "", "Blacklist of operation types to block (e.g., \"CDUA\" for Create, Delete, Update, Activate)")
	rootCmd.Flags().StringSliceVar(&cfg.AllowedPackages, "allowed-packages", nil, "Restrict operations to specific packages (comma-separated, supports wildcards like Z*)")
	rootCmd.Flags().BoolVar(&cfg.EnableTransports, "enable-transports", false, "Enable transport management operations (disabled by default for safety)")
	rootCmd.Flags().BoolVar(&cfg.TransportReadOnly, "transport-read-only", false, "Only allow read operations on transports (list, get)")
	rootCmd.Flags().StringSliceVar(&cfg.AllowedTransports, "allowed-transports", nil, "Restrict transport operations to specific transports (comma-separated, supports wildcards like A4HK*)")
	rootCmd.Flags().BoolVar(&cfg.AllowTransportableEdits, "allow-transportable-edits", false, "Allow editing objects in transportable packages (requires transport parameter)")

	// Mode options
	rootCmd.Flags().StringVar(&cfg.Mode, "mode", "focused", "Tool mode: focused (81 essential tools) or expert (all 122 tools)")
	rootCmd.Flags().StringVar(&cfg.DisabledGroups, "disabled-groups", "", "Disable tool groups: 5/U=UI5, T=Tests, H=HANA, D=Debug (e.g., \"TH\" disables Tests and HANA)")

	// Feature configuration (safety network)
	// Values: "auto" (default), "on", "off"
	rootCmd.Flags().StringVar(&cfg.FeatureHANA, "feature-hana", "auto", "HANA database detection: auto, on, off")
	rootCmd.Flags().StringVar(&cfg.FeatureAbapGit, "feature-abapgit", "auto", "abapGit integration: auto, on, off")
	rootCmd.Flags().StringVar(&cfg.FeatureRAP, "feature-rap", "auto", "RAP/OData development: auto, on, off")
	rootCmd.Flags().StringVar(&cfg.FeatureAMDP, "feature-amdp", "auto", "AMDP/HANA debugger: auto, on, off")
	rootCmd.Flags().StringVar(&cfg.FeatureUI5, "feature-ui5", "auto", "UI5/Fiori BSP management: auto, on, off")
	rootCmd.Flags().StringVar(&cfg.FeatureTransport, "feature-transport", "auto", "CTS transport management: auto, on, off")

	// Debugger configuration
	rootCmd.Flags().StringVar(&cfg.TerminalID, "terminal-id", "", "SAP GUI terminal ID for cross-tool breakpoint sharing")

	// RFC connection settings
	rootCmd.Flags().StringVar(&cfg.ConnectionMode, "connection-mode", "http", "Connection mode: http (default) or rfc")
	rootCmd.Flags().StringVar(&cfg.AsHost, "ashost", "", "SAP application server hostname (RFC mode)")
	rootCmd.Flags().StringVar(&cfg.SysNr, "sysnr", "00", "SAP system number (RFC mode)")
	rootCmd.Flags().StringVar(&cfg.MsHost, "mshost", "", "SAP message server host (RFC load balancing)")
	rootCmd.Flags().StringVar(&cfg.MsServ, "msserv", "", "SAP message server service/port (RFC load balancing)")
	rootCmd.Flags().StringVar(&cfg.R3Name, "r3name", "", "SAP system name (RFC load balancing)")
	rootCmd.Flags().StringVar(&cfg.Group, "group", "", "SAP logon group (RFC load balancing)")
	rootCmd.Flags().StringVar(&cfg.JcoProxyJar, "jco-proxy-jar", "", "Path to jco-proxy JAR file")
	rootCmd.Flags().StringVar(&cfg.JcoLibsDir, "jco-libs-dir", "", "Path to JCo libraries directory")
	rootCmd.Flags().StringVar(&cfg.JavaPath, "java-path", "java", "Path to Java binary")
	rootCmd.Flags().IntVar(&cfg.RfcProxyPort, "rfc-proxy-port", 0, "Fixed sidecar port (0=auto)")
	rootCmd.Flags().IntVar(&cfg.RfcMaxConcurrent, "rfc-max-concurrent", 5, "Max concurrent RFC calls")

	// Output options
	rootCmd.Flags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "Enable verbose output to stderr")

	// Bind flags to viper for environment variable support
	viper.BindPFlag("url", rootCmd.Flags().Lookup("url"))
	viper.BindPFlag("user", rootCmd.Flags().Lookup("user"))
	viper.BindPFlag("password", rootCmd.Flags().Lookup("password"))
	viper.BindPFlag("client", rootCmd.Flags().Lookup("client"))
	viper.BindPFlag("language", rootCmd.Flags().Lookup("language"))
	viper.BindPFlag("insecure", rootCmd.Flags().Lookup("insecure"))
	viper.BindPFlag("cookie-file", rootCmd.Flags().Lookup("cookie-file"))
	viper.BindPFlag("cookie-string", rootCmd.Flags().Lookup("cookie-string"))
	viper.BindPFlag("read-only", rootCmd.Flags().Lookup("read-only"))
	viper.BindPFlag("block-free-sql", rootCmd.Flags().Lookup("block-free-sql"))
	viper.BindPFlag("allowed-ops", rootCmd.Flags().Lookup("allowed-ops"))
	viper.BindPFlag("disallowed-ops", rootCmd.Flags().Lookup("disallowed-ops"))
	viper.BindPFlag("allowed-packages", rootCmd.Flags().Lookup("allowed-packages"))
	viper.BindPFlag("enable-transports", rootCmd.Flags().Lookup("enable-transports"))
	viper.BindPFlag("transport-read-only", rootCmd.Flags().Lookup("transport-read-only"))
	viper.BindPFlag("allowed-transports", rootCmd.Flags().Lookup("allowed-transports"))
	viper.BindPFlag("allow-transportable-edits", rootCmd.Flags().Lookup("allow-transportable-edits"))
	viper.BindPFlag("mode", rootCmd.Flags().Lookup("mode"))
	viper.BindPFlag("disabled-groups", rootCmd.Flags().Lookup("disabled-groups"))
	viper.BindPFlag("verbose", rootCmd.Flags().Lookup("verbose"))

	// Feature configuration
	viper.BindPFlag("feature-hana", rootCmd.Flags().Lookup("feature-hana"))
	viper.BindPFlag("feature-abapgit", rootCmd.Flags().Lookup("feature-abapgit"))
	viper.BindPFlag("feature-rap", rootCmd.Flags().Lookup("feature-rap"))
	viper.BindPFlag("feature-amdp", rootCmd.Flags().Lookup("feature-amdp"))
	viper.BindPFlag("feature-ui5", rootCmd.Flags().Lookup("feature-ui5"))
	viper.BindPFlag("feature-transport", rootCmd.Flags().Lookup("feature-transport"))

	// Debugger configuration
	viper.BindPFlag("terminal-id", rootCmd.Flags().Lookup("terminal-id"))

	// RFC connection settings
	viper.BindPFlag("connection-mode", rootCmd.Flags().Lookup("connection-mode"))
	viper.BindPFlag("ashost", rootCmd.Flags().Lookup("ashost"))
	viper.BindPFlag("sysnr", rootCmd.Flags().Lookup("sysnr"))
	viper.BindPFlag("mshost", rootCmd.Flags().Lookup("mshost"))
	viper.BindPFlag("msserv", rootCmd.Flags().Lookup("msserv"))
	viper.BindPFlag("r3name", rootCmd.Flags().Lookup("r3name"))
	viper.BindPFlag("group", rootCmd.Flags().Lookup("group"))
	viper.BindPFlag("jco-proxy-jar", rootCmd.Flags().Lookup("jco-proxy-jar"))
	viper.BindPFlag("jco-libs-dir", rootCmd.Flags().Lookup("jco-libs-dir"))
	viper.BindPFlag("java-path", rootCmd.Flags().Lookup("java-path"))
	viper.BindPFlag("rfc-proxy-port", rootCmd.Flags().Lookup("rfc-proxy-port"))
	viper.BindPFlag("rfc-max-concurrent", rootCmd.Flags().Lookup("rfc-max-concurrent"))

	// Set up environment variable mapping
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.SetEnvPrefix("SAP")
}

func runServer(cmd *cobra.Command, args []string) error {
	// Resolve configuration with priority: flags > env vars > defaults
	resolveConfig(cmd)

	// Validate configuration
	if err := validateConfig(); err != nil {
		return err
	}

	// Process cookie authentication
	if err := processCookieAuth(cmd); err != nil {
		return err
	}

	// Set verbose log output for feature probing
	if cfg.Verbose {
		adt.SetLogOutput(os.Stderr)
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[VERBOSE] Starting vsp server\n")
		fmt.Fprintf(os.Stderr, "[VERBOSE] Mode: %s\n", cfg.Mode)
		if cfg.DisabledGroups != "" {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Disabled groups: %s (5/U=UI5, T=Tests, H=HANA, D=Debug)\n", cfg.DisabledGroups)
		}
		if strings.EqualFold(cfg.ConnectionMode, "rfc") {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Connection: RFC mode\n")
			if cfg.AsHost != "" {
				fmt.Fprintf(os.Stderr, "[VERBOSE] RFC: Direct connection to %s (sysnr: %s)\n", cfg.AsHost, cfg.SysNr)
			} else if cfg.MsHost != "" {
				fmt.Fprintf(os.Stderr, "[VERBOSE] RFC: Load balanced via %s (r3name: %s, group: %s)\n", cfg.MsHost, cfg.R3Name, cfg.Group)
			}
			fmt.Fprintf(os.Stderr, "[VERBOSE] JCo proxy JAR: %s\n", cfg.JcoProxyJar)
			fmt.Fprintf(os.Stderr, "[VERBOSE] JCo libs dir: %s\n", cfg.JcoLibsDir)
		} else {
			fmt.Fprintf(os.Stderr, "[VERBOSE] SAP URL: %s\n", cfg.BaseURL)
		}
		fmt.Fprintf(os.Stderr, "[VERBOSE] SAP Client: %s\n", cfg.Client)
		fmt.Fprintf(os.Stderr, "[VERBOSE] SAP Language: %s\n", cfg.Language)
		if cfg.Username != "" {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Auth: Basic (user: %s)\n", cfg.Username)
		} else if len(cfg.Cookies) > 0 {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Auth: Cookie (%d cookies)\n", len(cfg.Cookies))
		}

		// Safety status
		if cfg.ReadOnly {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Safety: READ-ONLY mode enabled\n")
		}
		if cfg.BlockFreeSQL {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Safety: Free SQL queries BLOCKED\n")
		}
		if cfg.AllowedOps != "" {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Safety: Allowed operations: %s\n", cfg.AllowedOps)
		}
		if cfg.DisallowedOps != "" {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Safety: Disallowed operations: %s\n", cfg.DisallowedOps)
		}
		if len(cfg.AllowedPackages) > 0 {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Safety: Allowed packages: %v\n", cfg.AllowedPackages)
		}
		if cfg.EnableTransports {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Safety: Transport management ENABLED\n")
		}
		if cfg.AllowTransportableEdits {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Safety: Transportable edits ENABLED (can modify non-local objects)\n")
		}
		if !cfg.ReadOnly && !cfg.BlockFreeSQL && cfg.AllowedOps == "" && cfg.DisallowedOps == "" && len(cfg.AllowedPackages) == 0 {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Safety: UNRESTRICTED (no safety checks active)\n")
		}
	}

	// Load granular tool visibility from .vsp.json if present
	if systemsCfg, configPath, err := config.LoadSystems(); err == nil && systemsCfg != nil {
		if systemsCfg.Tools != nil {
			cfg.ToolsConfig = systemsCfg.Tools
			if cfg.Verbose {
				enabled := 0
				disabled := 0
				for _, v := range systemsCfg.Tools {
					if v {
						enabled++
					} else {
						disabled++
					}
				}
				fmt.Fprintf(os.Stderr, "[VERBOSE] Tool config loaded from %s: %d enabled, %d disabled\n", configPath, enabled, disabled)
			}
		}
	}

	// Create and start MCP server
	server := mcp.NewServer(cfg)
	return server.ServeStdio()
}

func resolveConfig(cmd *cobra.Command) {
	// If --system flag is specified, load system config from .vsp.json first.
	// System profile values act as base defaults; CLI flags can still override.
	if systemName != "" {
		sysCfg, configPath, err := config.LoadSystems()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] Failed to load systems config: %v\n", err)
		} else if sysCfg == nil {
			fmt.Fprintf(os.Stderr, "[WARN] --system '%s' specified but no .vsp.json found\n", systemName)
		} else {
			sys, err := sysCfg.GetSystem(systemName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[WARN] %v\n", err)
			} else {
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "[VERBOSE] Loading system '%s' from %s\n", systemName, configPath)
				}
				// Apply system config as defaults (CLI flags already set on cfg take precedence)
				if cfg.BaseURL == "" {
					cfg.BaseURL = sys.URL
				}
				if cfg.Username == "" {
					cfg.Username = sys.User
				}
				if cfg.Password == "" {
					cfg.Password = sys.Password
				}
				if !cmd.Flags().Changed("client") && sys.Client != "" {
					cfg.Client = sys.Client
				}
				if !cmd.Flags().Changed("language") && sys.Language != "" {
					cfg.Language = sys.Language
				}
				if !cmd.Flags().Changed("insecure") && sys.Insecure {
					cfg.InsecureSkipVerify = true
				}
				// RFC connection settings from system profile
				if !cmd.Flags().Changed("connection-mode") && sys.ConnectionMode != "" {
					cfg.ConnectionMode = sys.ConnectionMode
				}
				if !cmd.Flags().Changed("ashost") && sys.AsHost != "" {
					cfg.AsHost = sys.AsHost
				}
				if !cmd.Flags().Changed("sysnr") && sys.SysNr != "" {
					cfg.SysNr = sys.SysNr
				}
				if !cmd.Flags().Changed("mshost") && sys.MsHost != "" {
					cfg.MsHost = sys.MsHost
				}
				if !cmd.Flags().Changed("msserv") && sys.MsServ != "" {
					cfg.MsServ = sys.MsServ
				}
				if !cmd.Flags().Changed("r3name") && sys.R3Name != "" {
					cfg.R3Name = sys.R3Name
				}
				if !cmd.Flags().Changed("group") && sys.Group != "" {
					cfg.Group = sys.Group
				}
				if !cmd.Flags().Changed("jco-libs-dir") && sys.JcoLibsDir != "" {
					cfg.JcoLibsDir = sys.JcoLibsDir
				}
				if !cmd.Flags().Changed("jco-proxy-jar") && sys.JcoProxyJar != "" {
					cfg.JcoProxyJar = sys.JcoProxyJar
				}
				if sys.JavaPath != "" && !cmd.Flags().Changed("java-path") {
					cfg.JavaPath = sys.JavaPath
				}
				// Cookie auth from system profile
				if sys.CookieFile != "" {
					cookies, err := adt.LoadCookiesFromFile(sys.CookieFile)
					if err == nil && len(cookies) > 0 {
						cfg.Cookies = cookies
					}
				}
				if sys.CookieString != "" {
					cookies := adt.ParseCookieString(sys.CookieString)
					if len(cookies) > 0 {
						cfg.Cookies = cookies
					}
				}
				// Safety settings from system profile
				if sys.ReadOnly {
					cfg.ReadOnly = true
				}
				if len(sys.AllowedPackages) > 0 && len(cfg.AllowedPackages) == 0 {
					cfg.AllowedPackages = sys.AllowedPackages
				}
			}
		}
	}

	// Check if cookie auth is explicitly requested via CLI flags OR env vars
	// If so, we should NOT load user/password from env/.env to avoid conflicts
	// Cookie auth takes precedence over basic auth since it's more explicit
	cookieAuthViaCLI := cmd.Flags().Changed("cookie-file") || cmd.Flags().Changed("cookie-string")
	cookieAuthViaEnv := viper.GetString("COOKIE_FILE") != "" || viper.GetString("COOKIE_STRING") != ""
	hasCookieAuth := cookieAuthViaCLI || cookieAuthViaEnv || len(cfg.Cookies) > 0

	// URL: flag > system profile > SAP_URL env
	if cfg.BaseURL == "" {
		cfg.BaseURL = viper.GetString("URL")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = viper.GetString("SERVICE_URL")
	}

	// Username: flag > system profile > SAP_USER env (skip if cookie auth is present)
	if cfg.Username == "" && !hasCookieAuth {
		cfg.Username = viper.GetString("USER")
	}
	if cfg.Username == "" && !hasCookieAuth {
		cfg.Username = viper.GetString("USERNAME")
	}

	// Password: flag > system profile > SAP_PASSWORD env (skip if cookie auth is present)
	if cfg.Password == "" && !hasCookieAuth {
		cfg.Password = viper.GetString("PASSWORD")
	}
	if cfg.Password == "" && !hasCookieAuth {
		cfg.Password = viper.GetString("PASS")
	}

	// Client: flag > system profile > SAP_CLIENT env > default
	if !cmd.Flags().Changed("client") && systemName == "" {
		if envClient := viper.GetString("CLIENT"); envClient != "" {
			cfg.Client = envClient
		}
	}

	// Language: flag > system profile > SAP_LANGUAGE env > default
	if !cmd.Flags().Changed("language") && systemName == "" {
		if envLang := viper.GetString("LANGUAGE"); envLang != "" {
			cfg.Language = envLang
		}
	}

	// Insecure: flag > system profile > SAP_INSECURE env
	if !cmd.Flags().Changed("insecure") && systemName == "" {
		cfg.InsecureSkipVerify = viper.GetBool("INSECURE")
	}

	// Mode: flag > SAP_MODE env > default (focused)
	if !cmd.Flags().Changed("mode") {
		if envMode := viper.GetString("MODE"); envMode != "" {
			cfg.Mode = envMode
		}
	}

	// DisabledGroups: flag > SAP_DISABLED_GROUPS env
	if !cmd.Flags().Changed("disabled-groups") {
		if envGroups := viper.GetString("DISABLED_GROUPS"); envGroups != "" {
			cfg.DisabledGroups = envGroups
		}
	}

	// Verbose: flag > SAP_VERBOSE env
	if !cmd.Flags().Changed("verbose") {
		cfg.Verbose = viper.GetBool("VERBOSE")
	}

	// Safety options: flag > SAP_* env
	if !cmd.Flags().Changed("read-only") {
		cfg.ReadOnly = viper.GetBool("READ_ONLY")
	}
	if !cmd.Flags().Changed("block-free-sql") {
		cfg.BlockFreeSQL = viper.GetBool("BLOCK_FREE_SQL")
	}
	if !cmd.Flags().Changed("allowed-ops") {
		cfg.AllowedOps = viper.GetString("ALLOWED_OPS")
	}
	if !cmd.Flags().Changed("disallowed-ops") {
		cfg.DisallowedOps = viper.GetString("DISALLOWED_OPS")
	}
	if !cmd.Flags().Changed("allowed-packages") {
		// Use GetString and split manually - GetStringSlice doesn't split comma-separated env vars
		if pkgStr := viper.GetString("ALLOWED_PACKAGES"); pkgStr != "" {
			cfg.AllowedPackages = splitCommaSeparated(pkgStr)
		}
	}
	if !cmd.Flags().Changed("enable-transports") {
		cfg.EnableTransports = viper.GetBool("ENABLE_TRANSPORTS")
	}
	if !cmd.Flags().Changed("transport-read-only") {
		cfg.TransportReadOnly = viper.GetBool("TRANSPORT_READ_ONLY")
	}
	if !cmd.Flags().Changed("allowed-transports") {
		// Use GetString and split manually - GetStringSlice doesn't split comma-separated env vars
		if transportStr := viper.GetString("ALLOWED_TRANSPORTS"); transportStr != "" {
			cfg.AllowedTransports = splitCommaSeparated(transportStr)
		}
	}
	if !cmd.Flags().Changed("allow-transportable-edits") {
		cfg.AllowTransportableEdits = viper.GetBool("ALLOW_TRANSPORTABLE_EDITS")
	}

	// Feature configuration: flag > SAP_FEATURE_* env
	if !cmd.Flags().Changed("feature-hana") {
		if v := viper.GetString("FEATURE_HANA"); v != "" {
			cfg.FeatureHANA = v
		}
	}
	if !cmd.Flags().Changed("feature-abapgit") {
		if v := viper.GetString("FEATURE_ABAPGIT"); v != "" {
			cfg.FeatureAbapGit = v
		}
	}
	if !cmd.Flags().Changed("feature-rap") {
		if v := viper.GetString("FEATURE_RAP"); v != "" {
			cfg.FeatureRAP = v
		}
	}
	if !cmd.Flags().Changed("feature-amdp") {
		if v := viper.GetString("FEATURE_AMDP"); v != "" {
			cfg.FeatureAMDP = v
		}
	}
	if !cmd.Flags().Changed("feature-ui5") {
		if v := viper.GetString("FEATURE_UI5"); v != "" {
			cfg.FeatureUI5 = v
		}
	}
	if !cmd.Flags().Changed("feature-transport") {
		if v := viper.GetString("FEATURE_TRANSPORT"); v != "" {
			cfg.FeatureTransport = v
		}
	}

	// Terminal ID for debugger: flag > SAP_TERMINAL_ID env
	if !cmd.Flags().Changed("terminal-id") {
		if v := viper.GetString("TERMINAL_ID"); v != "" {
			cfg.TerminalID = v
		}
	}

	// RFC settings: flag > system profile > SAP_* env
	// When --system is used, system profile already set these; don't let env vars override
	if !cmd.Flags().Changed("connection-mode") && cfg.ConnectionMode == "" {
		if v := viper.GetString("CONNECTION_MODE"); v != "" {
			cfg.ConnectionMode = v
		}
	}
	if !cmd.Flags().Changed("ashost") && cfg.AsHost == "" {
		if v := viper.GetString("ASHOST"); v != "" {
			cfg.AsHost = v
		}
	}
	if !cmd.Flags().Changed("sysnr") && cfg.SysNr == "" {
		if v := viper.GetString("SYSNR"); v != "" {
			cfg.SysNr = v
		}
	}
	if !cmd.Flags().Changed("mshost") && cfg.MsHost == "" {
		if v := viper.GetString("MSHOST"); v != "" {
			cfg.MsHost = v
		}
	}
	if !cmd.Flags().Changed("msserv") && cfg.MsServ == "" {
		if v := viper.GetString("MSSERV"); v != "" {
			cfg.MsServ = v
		}
	}
	if !cmd.Flags().Changed("r3name") && cfg.R3Name == "" {
		if v := viper.GetString("R3NAME"); v != "" {
			cfg.R3Name = v
		}
	}
	if !cmd.Flags().Changed("group") && cfg.Group == "" {
		if v := viper.GetString("GROUP"); v != "" {
			cfg.Group = v
		}
	}
	if !cmd.Flags().Changed("jco-proxy-jar") && cfg.JcoProxyJar == "" {
		if v := viper.GetString("JCO_PROXY_JAR"); v != "" {
			cfg.JcoProxyJar = v
		}
	}
	if !cmd.Flags().Changed("jco-libs-dir") && cfg.JcoLibsDir == "" {
		if v := viper.GetString("JCO_LIBS_DIR"); v != "" {
			cfg.JcoLibsDir = v
		}
	}
	if !cmd.Flags().Changed("java-path") && cfg.JavaPath == "" {
		if v := viper.GetString("JAVA_PATH"); v != "" {
			cfg.JavaPath = v
		}
	}
	if !cmd.Flags().Changed("rfc-proxy-port") && cfg.RfcProxyPort == 0 {
		if v := viper.GetInt("RFC_PROXY_PORT"); v != 0 {
			cfg.RfcProxyPort = v
		}
	}
	if !cmd.Flags().Changed("rfc-max-concurrent") && cfg.RfcMaxConcurrent == 0 {
		if v := viper.GetInt("RFC_MAX_CONCURRENT"); v != 0 {
			cfg.RfcMaxConcurrent = v
		}
	}
}

func validateConfig() error {
	// In RFC mode, URL is not required; RFC connection params are
	if strings.EqualFold(cfg.ConnectionMode, "rfc") {
		hasDirect := cfg.AsHost != ""
		hasLB := cfg.MsHost != ""
		if !hasDirect && !hasLB {
			return fmt.Errorf("RFC mode requires --ashost or --mshost")
		}
		if hasDirect && hasLB {
			return fmt.Errorf("cannot specify both --ashost (direct) and --mshost (load balancing)")
		}
		if hasDirect && cfg.SysNr == "" {
			return fmt.Errorf("--sysnr required for direct RFC connection")
		}
		if hasLB {
			if cfg.MsServ == "" {
				return fmt.Errorf("--msserv required for RFC load balancing")
			}
			if cfg.R3Name == "" {
				return fmt.Errorf("--r3name required for RFC load balancing")
			}
			if cfg.Group == "" {
				return fmt.Errorf("--group required for RFC load balancing")
			}
		}
	} else {
		// HTTP mode requires URL
		if cfg.BaseURL == "" {
			return fmt.Errorf("SAP URL is required. Use --url flag or SAP_URL environment variable")
		}
	}

	// Validate mode
	if cfg.Mode != "focused" && cfg.Mode != "expert" {
		return fmt.Errorf("invalid mode: %s (must be 'focused' or 'expert')", cfg.Mode)
	}

	// Check if we have either basic auth or cookies will be processed
	// Cookies are checked later in processCookieAuth
	return nil
}

func processCookieAuth(cmd *cobra.Command) error {
	cookieFile, _ := cmd.Flags().GetString("cookie-file")
	cookieString, _ := cmd.Flags().GetString("cookie-string")

	// Check environment variables if flags not provided
	if cookieFile == "" {
		cookieFile = viper.GetString("COOKIE_FILE")
	}
	if cookieString == "" {
		cookieString = viper.GetString("COOKIE_STRING")
	}

	// Count authentication methods
	authMethods := 0
	if cfg.Username != "" && cfg.Password != "" {
		authMethods++
	}
	if cookieFile != "" {
		authMethods++
	}
	if cookieString != "" {
		authMethods++
	}

	if authMethods > 1 {
		return fmt.Errorf("only one authentication method can be used at a time (basic auth, cookie-file, or cookie-string)")
	}

	if authMethods == 0 {
		return fmt.Errorf("authentication required. Use --user/--password, --cookie-file, or --cookie-string")
	}

	// Process cookie file
	if cookieFile != "" {
		if _, err := os.Stat(cookieFile); os.IsNotExist(err) {
			return fmt.Errorf("cookie file not found: %s", cookieFile)
		}

		cookies, err := adt.LoadCookiesFromFile(cookieFile)
		if err != nil {
			return fmt.Errorf("failed to load cookies from file: %w", err)
		}

		if len(cookies) == 0 {
			return fmt.Errorf("no cookies found in file: %s", cookieFile)
		}

		cfg.Cookies = cookies
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Loaded %d cookies from file: %s\n", len(cookies), cookieFile)
		}
	}

	// Process cookie string
	if cookieString != "" {
		cookies := adt.ParseCookieString(cookieString)
		if len(cookies) == 0 {
			return fmt.Errorf("failed to parse cookie string")
		}

		cfg.Cookies = cookies
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "[VERBOSE] Parsed %d cookies from string\n", len(cookies))
		}
	}

	return nil
}

// splitCommaSeparated splits a comma-separated string into a slice, trimming whitespace.
// This is needed because viper.GetStringSlice doesn't properly split comma-separated env vars.
func splitCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
