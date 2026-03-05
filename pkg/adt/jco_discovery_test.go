package adt

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultSearchPaths(t *testing.T) {
	paths := DefaultSearchPaths()
	if len(paths) == 0 {
		t.Fatal("expected at least one search path")
	}
	if paths[0] != "./jco-libs" {
		t.Errorf("first path should be ./jco-libs, got %s", paths[0])
	}
}

func TestDefaultSearchPaths_EnvVar(t *testing.T) {
	t.Setenv("SAP_JCO_LIBS_DIR", "/custom/jco")
	paths := DefaultSearchPaths()
	found := false
	for _, p := range paths {
		if p == "/custom/jco" {
			found = true
			break
		}
	}
	if !found {
		t.Error("SAP_JCO_LIBS_DIR path not found in search paths")
	}
}

func TestDiscoverJCoLibs_LocalDir(t *testing.T) {
	tmpDir := t.TempDir()
	// Create mock JAR files
	os.WriteFile(filepath.Join(tmpDir, "com.sap.conn.jco_3.1.12.jar"), []byte("mock"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "com.sap.conn.jco.macosx.aarch64_3.1.12.jar"), []byte("mock"), 0644)

	result, err := DiscoverJCoLibs([]string{tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Version != "3.1.12" {
		t.Errorf("version = %s, want 3.1.12", result.Version)
	}
	if result.Platform != "macosx" {
		t.Errorf("platform = %s, want macosx", result.Platform)
	}
	if result.Arch != "aarch64" {
		t.Errorf("arch = %s, want aarch64", result.Arch)
	}
	if result.SourceDir != tmpDir {
		t.Errorf("sourceDir = %s, want %s", result.SourceDir, tmpDir)
	}
}

func TestDiscoverJCoLibs_OnlyMainJar(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "com.sap.conn.jco_3.1.10.jar"), []byte("mock"), 0644)

	result, err := DiscoverJCoLibs([]string{tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Version != "3.1.10" {
		t.Errorf("version = %s, want 3.1.10", result.Version)
	}
	if result.NativeJar != "" {
		t.Errorf("expected empty NativeJar, got %s", result.NativeJar)
	}
}

func TestDiscoverJCoLibs_NotFound(t *testing.T) {
	result, err := DiscoverJCoLibs([]string{"/nonexistent/path"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for nonexistent path")
	}
}

func TestDiscoverJCoLibs_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	result, err := DiscoverJCoLibs([]string{tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for empty dir")
	}
}

func TestDiscoverJCoLibs_Priority(t *testing.T) {
	// First dir has v3.1.10, second has v3.1.12
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir1, "com.sap.conn.jco_3.1.10.jar"), []byte("mock"), 0644)
	os.WriteFile(filepath.Join(dir2, "com.sap.conn.jco_3.1.12.jar"), []byte("mock"), 0644)

	result, err := DiscoverJCoLibs([]string{dir1, dir2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	// First match wins (priority order)
	if result.Version != "3.1.10" {
		t.Errorf("expected first dir to win (3.1.10), got %s", result.Version)
	}
}

func TestDiscoverJCoLibs_IgnoresSubdirs(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a subdirectory with a matching name (should be ignored)
	os.MkdirAll(filepath.Join(tmpDir, "com.sap.conn.jco_3.1.12.jar"), 0755)

	result, err := DiscoverJCoLibs([]string{tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when match is a directory")
	}
}

func TestDetectArchMismatch(t *testing.T) {
	tests := []struct {
		jcoArch  string
		javaArch string
		want     bool
	}{
		{"aarch64", "aarch64", false},
		{"x86_64", "x86_64", false},
		{"aarch64", "x86_64", true},
		{"x86_64", "amd64", false},  // amd64 = x86_64
		{"aarch64", "arm64", false}, // arm64 = aarch64
		{"x86_64", "aarch64", true},
		{"x86", "x86_64", true},
	}
	for _, tt := range tests {
		t.Run(tt.jcoArch+"_vs_"+tt.javaArch, func(t *testing.T) {
			got := DetectArchMismatch(tt.jcoArch, tt.javaArch)
			if got != tt.want {
				t.Errorf("DetectArchMismatch(%s, %s) = %v, want %v",
					tt.jcoArch, tt.javaArch, got, tt.want)
			}
		})
	}
}

func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"aarch64", "arm64"},
		{"arm64", "arm64"},
		{"x86_64", "amd64"},
		{"amd64", "amd64"},
		{"x64", "amd64"},
		{"x86", "x86"},
		{"i386", "x86"},
		{"i686", "x86"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeArch(tt.input)
			if got != tt.want {
				t.Errorf("normalizeArch(%s) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestCopyJCoLibs(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "dest")

	jcoJar := filepath.Join(srcDir, "com.sap.conn.jco_3.1.12.jar")
	nativeJar := filepath.Join(srcDir, "com.sap.conn.jco.macosx.aarch64_3.1.12.jar")
	os.WriteFile(jcoJar, []byte("jco-content"), 0644)
	os.WriteFile(nativeJar, []byte("native-content"), 0644)

	result := &JCoDiscoveryResult{
		JcoJar:    jcoJar,
		NativeJar: nativeJar,
	}

	if err := CopyJCoLibs(result, dstDir); err != nil {
		t.Fatalf("CopyJCoLibs failed: %v", err)
	}

	// Verify copies exist
	if _, err := os.Stat(filepath.Join(dstDir, "com.sap.conn.jco_3.1.12.jar")); err != nil {
		t.Error("JCo JAR not copied")
	}
	if _, err := os.Stat(filepath.Join(dstDir, "com.sap.conn.jco.macosx.aarch64_3.1.12.jar")); err != nil {
		t.Error("Native JAR not copied")
	}

	// Verify content
	data, _ := os.ReadFile(filepath.Join(dstDir, "com.sap.conn.jco_3.1.12.jar"))
	if string(data) != "jco-content" {
		t.Error("JCo JAR content mismatch")
	}
}

func TestCopyJCoLibs_CreatesDest(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "a", "b", "c")

	jcoJar := filepath.Join(srcDir, "test.jar")
	os.WriteFile(jcoJar, []byte("test"), 0644)

	result := &JCoDiscoveryResult{JcoJar: jcoJar}
	if err := CopyJCoLibs(result, dstDir); err != nil {
		t.Fatalf("CopyJCoLibs failed: %v", err)
	}

	if _, err := os.Stat(dstDir); err != nil {
		t.Error("destination directory not created")
	}
}

func TestExtractNativeLib(t *testing.T) {
	// Create a mock native JAR (ZIP) containing a native library
	tmpDir := t.TempDir()
	jarPath := filepath.Join(tmpDir, "native.jar")

	libName := nativeLibName()
	if libName == "" {
		t.Skip("unsupported platform")
	}

	// Create ZIP with the native lib inside
	f, err := os.Create(jarPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, err := w.Create(libName)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte("fake-native-lib"))
	w.Close()
	f.Close()

	destDir := filepath.Join(tmpDir, "extracted")
	os.MkdirAll(destDir, 0755)

	libPath, err := ExtractNativeLib(jarPath, destDir)
	if err != nil {
		t.Fatalf("ExtractNativeLib failed: %v", err)
	}

	if filepath.Base(libPath) != libName {
		t.Errorf("expected %s, got %s", libName, filepath.Base(libPath))
	}

	data, err := os.ReadFile(libPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fake-native-lib" {
		t.Error("extracted content mismatch")
	}
}

func TestExtractNativeLib_NotInJar(t *testing.T) {
	tmpDir := t.TempDir()
	jarPath := filepath.Join(tmpDir, "empty.jar")

	// Create empty ZIP
	f, _ := os.Create(jarPath)
	w := zip.NewWriter(f)
	w.Close()
	f.Close()

	_, err := ExtractNativeLib(jarPath, tmpDir)
	if err == nil {
		t.Error("expected error for missing native lib")
	}
}

func TestExtractNativeLib_InvalidJar(t *testing.T) {
	tmpDir := t.TempDir()
	jarPath := filepath.Join(tmpDir, "bad.jar")
	os.WriteFile(jarPath, []byte("not-a-zip"), 0644)

	_, err := ExtractNativeLib(jarPath, tmpDir)
	if err == nil {
		t.Error("expected error for invalid JAR")
	}
}

func TestValidateJava(t *testing.T) {
	// Only run if java is available
	info, err := ValidateJava("java")
	if err != nil {
		t.Skipf("java not available: %v", err)
	}
	if info.Version == "" {
		t.Error("expected version")
	}
	if info.Arch == "" {
		t.Error("expected arch")
	}
	t.Logf("Java %s (%s) at %s", info.Version, info.Arch, info.Path)
}

func TestValidateJava_NotFound(t *testing.T) {
	_, err := ValidateJava("/nonexistent/java")
	if err == nil {
		t.Error("expected error for nonexistent java")
	}
}

func TestNativeLibName(t *testing.T) {
	name := nativeLibName()
	switch runtime.GOOS {
	case "darwin":
		if name != "libsapjco3.dylib" {
			t.Errorf("expected libsapjco3.dylib, got %s", name)
		}
	case "linux":
		if name != "libsapjco3.so" {
			t.Errorf("expected libsapjco3.so, got %s", name)
		}
	case "windows":
		if name != "sapjco3.dll" {
			t.Errorf("expected sapjco3.dll, got %s", name)
		}
	}
}

func TestCopyFile_SameFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(path, []byte("content"), 0644)

	// Copying to self should be a no-op
	if err := copyFile(path, path); err != nil {
		t.Fatalf("copyFile to self should succeed: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "content" {
		t.Error("content should be unchanged")
	}
}
