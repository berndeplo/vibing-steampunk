package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	deps "github.com/oisee/vibing-steampunk/embedded/deps"
	"github.com/oisee/vibing-steampunk/pkg/adt"
	"github.com/oisee/vibing-steampunk/pkg/config"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(jcoCmd)
	jcoCmd.AddCommand(jcoSetupCmd)
	jcoCmd.AddCommand(jcoStatusCmd)

	jcoSetupCmd.Flags().StringP("system", "s", "", "System name in .vsp.json to update (e.g., 'bd1')")
}

var jcoCmd = &cobra.Command{
	Use:   "jco",
	Short: "Manage JCo libraries for RFC mode",
	Long:  `Manage SAP JCo libraries required for RFC connection mode.`,
}

var jcoSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive JCo library setup",
	Long: `Interactively discover, validate, and configure JCo libraries.

Steps:
  1. Search standard Eclipse plugin locations for JCo JARs
  2. If not found, prompt for manual path
  3. Copy libraries to ./jco-libs/ for portability
  4. Extract native library from platform JAR
  5. Validate Java installation and architecture
  6. Extract embedded jco-proxy.jar
  7. Update .vsp.json (if --system specified) or print config`,
	RunE: runJcoSetup,
}

var jcoStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check JCo setup status",
	Long:  `Non-interactive check of JCo library availability and configuration.`,
	RunE:  runJcoStatus,
}

func runJcoSetup(cmd *cobra.Command, args []string) error {
	fmt.Println("=== JCo Setup Wizard ===")
	fmt.Println()

	destDir := "./jco-libs"

	// Step 1: Auto-discover
	fmt.Println("Step 1: Searching for JCo libraries...")
	paths := adt.DefaultSearchPaths()
	result, err := adt.DiscoverJCoLibs(paths)
	if err != nil {
		return fmt.Errorf("discovery error: %w", err)
	}

	// Step 2: If not found, interactive prompt
	if result == nil {
		fmt.Println("\nJCo libraries not found in standard locations.")
		fmt.Println("\nSearched:")
		for _, p := range paths {
			fmt.Printf("  - %s\n", p)
		}
		fmt.Println("\nThe libraries are in your Eclipse ADT plugins folder.")
		fmt.Println("Look for files matching: com.sap.conn.jco_*.jar")
		fmt.Print("\nEnter Eclipse plugins folder path (or press Enter to skip): ")

		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			manualPath := strings.TrimSpace(scanner.Text())
			if manualPath != "" {
				result, err = adt.DiscoverJCoLibs([]string{manualPath})
				if err != nil {
					return fmt.Errorf("discovery error: %w", err)
				}
				if result == nil {
					fmt.Println("\nNo JCo libraries found at that path.")
					fmt.Println("Expected: com.sap.conn.jco_*.jar and com.sap.conn.jco.<platform>.<arch>_*.jar")
					return fmt.Errorf("JCo libraries not found")
				}
			}
		}

		if result == nil {
			return fmt.Errorf("JCo libraries not found. Install Eclipse ADT or download JCo from SAP Support Portal")
		}
	}

	fmt.Printf("\nFound JCo %s", result.Version)
	if result.Platform != "" {
		fmt.Printf(" (%s/%s)", result.Platform, result.Arch)
	}
	fmt.Printf(" in %s\n", result.SourceDir)
	fmt.Printf("  JCo JAR:    %s\n", filepath.Base(result.JcoJar))
	if result.NativeJar != "" {
		fmt.Printf("  Native JAR: %s\n", filepath.Base(result.NativeJar))
	}

	// Step 3: Copy to ./jco-libs/
	srcAbs, _ := filepath.Abs(result.SourceDir)
	dstAbs, _ := filepath.Abs(destDir)
	if srcAbs != dstAbs {
		fmt.Printf("\nStep 2: Copying to %s/ for portability...\n", destDir)
		if err := adt.CopyJCoLibs(result, destDir); err != nil {
			return fmt.Errorf("copy failed: %w", err)
		}
		fmt.Println("  Done.")
	} else {
		fmt.Println("\nStep 2: Libraries already in ./jco-libs/ (skipping copy)")
	}

	// Step 4: Extract native library
	if result.NativeJar != "" {
		fmt.Println("\nStep 3: Extracting native library...")
		nativeJarInDest := filepath.Join(destDir, filepath.Base(result.NativeJar))
		libPath, err := adt.ExtractNativeLib(nativeJarInDest, destDir)
		if err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}
		fmt.Printf("  Extracted: %s\n", filepath.Base(libPath))
		result.NativeLib = libPath
	} else {
		fmt.Println("\nStep 3: No native JAR found (skipping extraction)")
		fmt.Println("  You may need to place the native library manually in ./jco-libs/")
	}

	// Step 5: Validate Java
	fmt.Println("\nStep 4: Validating Java installation...")
	javaInfo, err := adt.ValidateJava("java")
	if err != nil {
		fmt.Printf("  WARNING: %v\n", err)
		fmt.Println("  Java 11+ is required. Install it or set JAVA_HOME.")
	} else {
		fmt.Printf("  Java %s (%s) at %s\n", javaInfo.Version, javaInfo.Arch, javaInfo.Path)

		if result.Arch != "" && adt.DetectArchMismatch(result.Arch, javaInfo.Arch) {
			fmt.Printf("  WARNING: Architecture mismatch! JCo is %s but Java is %s\n", result.Arch, javaInfo.Arch)
			fmt.Println("  Install matching Java or use different JCo libraries.")
		} else if result.Arch != "" {
			fmt.Println("  Architecture match: OK")
		}
	}

	// Step 6: Extract/locate proxy JAR
	fmt.Println("\nStep 5: Setting up jco-proxy...")
	proxyJar, err := extractOrFindProxyJar(destDir)
	if err != nil {
		fmt.Printf("  WARNING: %v\n", err)
	} else {
		fmt.Printf("  Proxy JAR: %s\n", proxyJar)
	}

	// Resolve absolute paths for config
	absDestDir, _ := filepath.Abs(destDir)

	// Step 7: Update .vsp.json or print instructions
	systemFlag, _ := cmd.Flags().GetString("system")
	if systemFlag != "" {
		fmt.Printf("\nStep 6: Updating .vsp.json system '%s'...\n", systemFlag)

		sysCfg, configPath, loadErr := config.LoadSystems()
		if loadErr != nil {
			fmt.Printf("  WARNING: Failed to load .vsp.json: %v\n", loadErr)
			fmt.Println("  Skipping config update.")
		} else if sysCfg == nil {
			// Create new config
			sysCfg = &config.SystemsConfig{
				Systems: make(map[string]config.SystemConfig),
			}
			configPath = ".vsp.json"
			fmt.Printf("  Creating new %s\n", configPath)
		}

		if sysCfg != nil {
			sys := sysCfg.Systems[systemFlag]

			// Update JCo paths
			sys.JcoLibsDir = absDestDir
			if proxyJar != "" {
				sys.JcoProxyJar = proxyJar
			}

			// Set connection mode to rfc if not already set
			if sys.ConnectionMode == "" {
				sys.ConnectionMode = "rfc"
			}

			sysCfg.Systems[systemFlag] = sys

			if saveErr := sysCfg.SaveToFile(configPath); saveErr != nil {
				fmt.Printf("  WARNING: Failed to save %s: %v\n", configPath, saveErr)
			} else {
				fmt.Printf("  Updated %s\n", configPath)
				fmt.Printf("    jco_libs_dir:  %s\n", absDestDir)
				if proxyJar != "" {
					fmt.Printf("    jco_proxy_jar: %s\n", proxyJar)
				}
			}
		}
	} else {
		fmt.Println("\nStep 6: Configuration")
	}

	// Summary
	fmt.Println("\n=== Setup Complete ===")
	fmt.Println()
	fmt.Printf("  JCo JAR:     %s\n", filepath.Join(destDir, filepath.Base(result.JcoJar)))
	if result.NativeJar != "" {
		fmt.Printf("  Native JAR:  %s\n", filepath.Join(destDir, filepath.Base(result.NativeJar)))
	}
	if result.NativeLib != "" {
		fmt.Printf("  Native lib:  %s\n", result.NativeLib)
	}
	if javaInfo != nil {
		fmt.Printf("  Java:        %s (%s, %s)\n", javaInfo.Path, javaInfo.Version, javaInfo.Arch)
	}
	if proxyJar != "" {
		fmt.Printf("  Proxy JAR:   %s\n", proxyJar)
	}

	if systemFlag == "" {
		fmt.Println("\nAdd to .env:")
		fmt.Println("  SAP_CONNECTION_MODE=rfc")
		fmt.Printf("  SAP_JCO_LIBS_DIR=%s\n", absDestDir)
		if proxyJar != "" {
			fmt.Printf("  SAP_JCO_PROXY_JAR=%s\n", proxyJar)
		}
		fmt.Println("\nOr re-run with --system to update .vsp.json directly:")
		fmt.Println("  vsp jco setup --system <name>")
	}

	return nil
}

func runJcoStatus(cmd *cobra.Command, args []string) error {
	fmt.Println("=== JCo Status ===")
	fmt.Println()

	allOK := true

	// Check ./jco-libs/
	result, _ := adt.DiscoverJCoLibs([]string{"./jco-libs"})
	if result != nil {
		fmt.Printf("  JCo:      %s v%s", filepath.Base(result.JcoJar), result.Version)
		if result.Platform != "" {
			fmt.Printf(" (%s/%s)", result.Platform, result.Arch)
		}
		fmt.Println()
		if result.NativeJar != "" {
			fmt.Printf("  Native:   %s\n", filepath.Base(result.NativeJar))
		} else {
			fmt.Println("  Native:   NOT FOUND (no platform-specific JAR)")
			allOK = false
		}
		if result.NativeLib != "" {
			fmt.Printf("  Lib:      %s\n", filepath.Base(result.NativeLib))
		} else {
			fmt.Println("  Lib:      NOT EXTRACTED (run 'vsp jco setup')")
			allOK = false
		}
	} else {
		fmt.Println("  JCo:      NOT FOUND in ./jco-libs/")
		fmt.Println("            Run 'vsp jco setup' to configure")
		allOK = false
	}

	// Check Java
	javaInfo, err := adt.ValidateJava("java")
	if err != nil {
		fmt.Printf("  Java:     NOT FOUND (%v)\n", err)
		allOK = false
	} else {
		fmt.Printf("  Java:     %s (%s) at %s\n", javaInfo.Version, javaInfo.Arch, javaInfo.Path)
		if result != nil && result.Arch != "" && adt.DetectArchMismatch(result.Arch, javaInfo.Arch) {
			fmt.Printf("  WARN:     Architecture mismatch (JCo=%s, Java=%s)\n", result.Arch, javaInfo.Arch)
			allOK = false
		}
	}

	// Check proxy JAR
	proxyJar := findLocalProxyJar()
	if proxyJar != "" {
		fmt.Printf("  Proxy:    %s\n", proxyJar)
	} else if deps.GetEmbeddedProxyJar() != nil {
		fmt.Println("  Proxy:    EMBEDDED (run 'vsp jco setup' to extract)")
		allOK = false
	} else {
		fmt.Println("  Proxy:    NOT FOUND")
		fmt.Println("            Run 'vsp jco setup' to extract")
		allOK = false
	}

	fmt.Println()
	if allOK {
		fmt.Println("  Status: READY")
	} else {
		fmt.Println("  Status: INCOMPLETE (run 'vsp jco setup' to fix)")
	}

	return nil
}

// extractOrFindProxyJar extracts the embedded proxy JAR to destDir, or finds it locally.
// Returns the absolute path to the proxy JAR.
func extractOrFindProxyJar(destDir string) (string, error) {
	destPath := filepath.Join(destDir, "jco-proxy.jar")

	// Check if already exists at destination
	if info, err := os.Stat(destPath); err == nil && info.Size() > 0 {
		abs, _ := filepath.Abs(destPath)
		fmt.Println("  Already exists (skipping extraction)")
		return abs, nil
	}

	// Extract from embedded data
	embeddedData := deps.GetEmbeddedProxyJar()
	if embeddedData != nil {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return "", fmt.Errorf("creating directory %s: %w", destDir, err)
		}
		if err := os.WriteFile(destPath, embeddedData, 0644); err != nil {
			return "", fmt.Errorf("extracting proxy JAR: %w", err)
		}
		abs, _ := filepath.Abs(destPath)
		fmt.Printf("  Extracted embedded proxy JAR (%d bytes)\n", len(embeddedData))
		return abs, nil
	}

	// Fallback: check dev build locations
	candidates := []string{
		"sidecar/jco-proxy/target/jco-proxy-1.0.0-shaded.jar",
		"sidecar/jco-proxy/dist/jco-proxy-1.0.0.jar",
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			abs, _ := filepath.Abs(path)
			return abs, nil
		}
	}

	return "", fmt.Errorf("proxy JAR not available (not embedded and not found locally)")
}

// findLocalProxyJar looks for the jco-proxy JAR in local locations (for status check).
func findLocalProxyJar() string {
	candidates := []string{
		"./jco-libs/jco-proxy.jar",
		"sidecar/jco-proxy/target/jco-proxy-1.0.0-shaded.jar",
		"sidecar/jco-proxy/dist/jco-proxy-1.0.0.jar",
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
