package adt

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// JCoDiscoveryResult holds the results of JCo library discovery.
type JCoDiscoveryResult struct {
	JcoJar    string // e.g., com.sap.conn.jco_3.1.12.jar
	NativeJar string // e.g., com.sap.conn.jco.macosx.aarch64_3.1.12.jar
	NativeLib string // e.g., libsapjco3.dylib (after extraction)
	Version   string // e.g., "3.1.12"
	Platform  string // e.g., "macosx", "win32", "linux"
	Arch      string // e.g., "aarch64", "x86_64"
	SourceDir string // Directory where JCo was found
}

// JavaInfo holds information about a Java installation.
type JavaInfo struct {
	Path    string // Path to java binary
	Version string // e.g., "21.0.1"
	Arch    string // e.g., "aarch64", "x86_64"
}

var (
	jcoJarPattern    = regexp.MustCompile(`^com\.sap\.conn\.jco_(\d+\.\d+\.\d+)\.jar$`)
	nativeJarPattern = regexp.MustCompile(`^com\.sap\.conn\.jco\.(\w+)\.(\w+)_(\d+\.\d+\.\d+)\.jar$`)
)

// DiscoverJCoLibs searches for JCo libraries in the given paths.
// Returns nil (without error) if not found.
func DiscoverJCoLibs(searchPaths []string) (*JCoDiscoveryResult, error) {
	for _, dir := range searchPaths {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // skip inaccessible directories
		}

		var result JCoDiscoveryResult
		result.SourceDir = dir

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()

			if m := jcoJarPattern.FindStringSubmatch(name); m != nil {
				result.JcoJar = filepath.Join(dir, name)
				result.Version = m[1]
			}

			if m := nativeJarPattern.FindStringSubmatch(name); m != nil {
				result.NativeJar = filepath.Join(dir, name)
				result.Platform = m[1]
				result.Arch = m[2]
				// native JAR version takes precedence if both present
				result.Version = m[3]
			}
		}

		// Need at least the main JCo JAR
		if result.JcoJar != "" {
			// Check for already-extracted native lib
			libName := nativeLibName()
			if libName != "" {
				libPath := filepath.Join(dir, libName)
				if _, err := os.Stat(libPath); err == nil {
					result.NativeLib = libPath
				}
			}
			return &result, nil
		}
	}
	return nil, nil
}

// DefaultSearchPaths returns the standard search paths for JCo libraries,
// ordered by priority: local > env > Eclipse standard locations.
func DefaultSearchPaths() []string {
	paths := []string{
		"./jco-libs", // Local (highest priority)
	}

	// Environment variables
	for _, env := range []string{"SAP_JCO_LIBS_DIR", "JCO_LIBS_PATH"} {
		if v := os.Getenv(env); v != "" {
			paths = append(paths, v)
		}
	}

	home, _ := os.UserHomeDir()
	if home == "" {
		return paths
	}

	switch runtime.GOOS {
	case "darwin":
		paths = append(paths,
			filepath.Join(home, ".p2/pool/plugins"),
			"/Applications/Eclipse.app/Contents/Eclipse/plugins",
			"/Applications/Eclipse ADT.app/Contents/Eclipse/plugins",
			filepath.Join(home, "Applications/Eclipse.app/Contents/Eclipse/plugins"),
			filepath.Join(home, "Applications/Eclipse ADT.app/Contents/Eclipse/plugins"),
			filepath.Join(home, "eclipse/java-latest/Eclipse.app/Contents/Eclipse/plugins"),
		)
	case "windows":
		appData := os.Getenv("APPDATA")
		localAppData := os.Getenv("LOCALAPPDATA")
		paths = append(paths,
			filepath.Join(home, ".p2/pool/plugins"),
		)
		if appData != "" {
			paths = append(paths, filepath.Join(appData, "eclipse/plugins"))
		}
		if localAppData != "" {
			paths = append(paths, filepath.Join(localAppData, "eclipse/plugins"))
		}
		paths = append(paths,
			`C:\Program Files\Eclipse\plugins`,
			`C:\Program Files\SAP\Eclipse\plugins`,
			`C:\Program Files (x86)\SAP\Eclipse\plugins`,
		)
	case "linux":
		paths = append(paths,
			filepath.Join(home, ".p2/pool/plugins"),
			filepath.Join(home, ".local/share/eclipse/plugins"),
			"/opt/eclipse/plugins",
			"/usr/local/eclipse/plugins",
			"/usr/share/eclipse/plugins",
		)
	}

	return paths
}

// CopyJCoLibs copies JCo libraries to a destination directory.
func CopyJCoLibs(result *JCoDiscoveryResult, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", destDir, err)
	}

	if result.JcoJar != "" {
		dest := filepath.Join(destDir, filepath.Base(result.JcoJar))
		if err := copyFile(result.JcoJar, dest); err != nil {
			return fmt.Errorf("copying JCo JAR: %w", err)
		}
	}

	if result.NativeJar != "" {
		dest := filepath.Join(destDir, filepath.Base(result.NativeJar))
		if err := copyFile(result.NativeJar, dest); err != nil {
			return fmt.Errorf("copying native JAR: %w", err)
		}
	}

	return nil
}

// ExtractNativeLib extracts the native library from the JCo native JAR.
// Returns the path to the extracted library.
func ExtractNativeLib(nativeJar, destDir string) (string, error) {
	libName := nativeLibName()
	if libName == "" {
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	r, err := zip.OpenReader(nativeJar)
	if err != nil {
		return "", fmt.Errorf("opening native JAR: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.Base(f.Name) == libName {
			destPath := filepath.Join(destDir, libName)
			if err := extractZipEntry(f, destPath); err != nil {
				return "", fmt.Errorf("extracting %s: %w", libName, err)
			}

			// macOS: fix dynamic linker path
			if runtime.GOOS == "darwin" {
				//nolint:gosec // install_name_tool is a standard macOS tool
				_ = exec.Command("install_name_tool", "-id", "@rpath/"+libName, destPath).Run()
			}

			return destPath, nil
		}
	}

	return "", fmt.Errorf("native library %s not found in %s", libName, filepath.Base(nativeJar))
}

// ValidateJava checks if Java is installed and returns version/architecture info.
func ValidateJava(javaPath string) (*JavaInfo, error) {
	if javaPath == "" {
		javaPath = "java"
	}

	// java -version outputs to stderr
	cmd := exec.Command(javaPath, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("java not found at %s: %w", javaPath, err)
	}

	text := string(output)
	info := &JavaInfo{Path: javaPath}

	// Parse version: look for patterns like "21.0.1", "17.0.2", "1.8.0_392"
	versionRe := regexp.MustCompile(`"(\d+[\d._]+)"`)
	if m := versionRe.FindStringSubmatch(text); m != nil {
		info.Version = m[1]
	}

	// Parse architecture from java -version output
	// Common patterns: "64-Bit", "aarch64", "x86_64", "amd64"
	textLower := strings.ToLower(text)
	switch {
	case strings.Contains(textLower, "aarch64"):
		info.Arch = "aarch64"
	case strings.Contains(textLower, "x86_64") || strings.Contains(textLower, "amd64"):
		info.Arch = "x86_64"
	case strings.Contains(textLower, "64-bit"):
		// Fallback: check system arch
		info.Arch = runtime.GOARCH
		if info.Arch == "arm64" {
			info.Arch = "aarch64"
		} else if info.Arch == "amd64" {
			info.Arch = "x86_64"
		}
	default:
		info.Arch = runtime.GOARCH
	}

	return info, nil
}

// DetectArchMismatch returns true if JCo and Java architectures don't match.
func DetectArchMismatch(jcoArch, javaArch string) bool {
	return normalizeArch(jcoArch) != normalizeArch(javaArch)
}

// nativeLibName returns the platform-specific native library filename.
func nativeLibName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libsapjco3.dylib"
	case "windows":
		return "sapjco3.dll"
	case "linux":
		return "libsapjco3.so"
	default:
		return ""
	}
}

// normalizeArch maps various architecture names to canonical forms.
func normalizeArch(arch string) string {
	switch strings.ToLower(arch) {
	case "aarch64", "arm64":
		return "arm64"
	case "x86_64", "amd64", "x64":
		return "amd64"
	case "x86", "i386", "i686":
		return "x86"
	default:
		return strings.ToLower(arch)
	}
}

// copyFile copies a file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	// Don't copy to self
	srcAbs, _ := filepath.Abs(src)
	dstAbs, _ := filepath.Abs(dst)
	if srcAbs == dstAbs {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	// Preserve source permissions
	info, err := os.Stat(src)
	if err == nil {
		_ = os.Chmod(dst, info.Mode())
	}

	return out.Close()
}

// extractZipEntry extracts a single zip file entry to destPath.
func extractZipEntry(f *zip.File, destPath string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return err
	}

	// Set executable permission for native libraries
	return os.Chmod(destPath, 0755)
}
