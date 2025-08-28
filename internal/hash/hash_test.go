package hash

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewCalculator(t *testing.T) {
	tests := []struct {
		name            string
		numWorkers      int
		expectedWorkers int
	}{
		{
			name:            "positive worker count",
			numWorkers:      4,
			expectedWorkers: 4,
		},
		{
			name:            "zero worker count defaults to GOMAXPROCS",
			numWorkers:      0,
			expectedWorkers: runtime.GOMAXPROCS(0),
		},
		{
			name:            "negative worker count defaults to GOMAXPROCS",
			numWorkers:      -1,
			expectedWorkers: runtime.GOMAXPROCS(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := NewCalculator(tt.numWorkers)
			if calc.numWorkers != tt.expectedWorkers {
				t.Errorf("NewCalculator(%d) numWorkers = %d, want %d",
					tt.numWorkers, calc.numWorkers, tt.expectedWorkers)
			}
		})
	}
}

func TestCalculateFileHash(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "empty file",
			content:  "",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "simple text",
			content:  "hello world",
			expected: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			name:     "text with newline",
			content:  "line1\nline2\n",
			expected: "2751a3a2f303ad21752038085e2b8c5f98ecff61a2e4ebbd43506a941725be80",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpfile, err := os.CreateTemp("", "test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(tmpfile.Name())

			if _, err := tmpfile.Write([]byte(tt.content)); err != nil {
				t.Fatal(err)
			}
			if err := tmpfile.Close(); err != nil {
				t.Fatal(err)
			}

			// Calculate hash
			calc := NewCalculator(0)
			hasher := sha256.New()
			buf := make([]byte, calc.bufferSize)
			ctx := context.Background()
			hash, err := calc.hashFileWithHasher(ctx, tmpfile.Name(), hasher, buf)
			if err != nil {
				t.Fatalf("hashFileWithHasher() error = %v", err)
			}

			if hash != tt.expected {
				t.Errorf("hashFileWithHasher() = %v, want %v", hash, tt.expected)
			}
		})
	}
}

func TestCalculateDirectory(t *testing.T) {
	calc := NewCalculator(0)
	ctx := context.Background()

	// Test with testdata directory
	result, err := calc.CalculateDirectory(ctx, "testdata/sample", nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() error = %v", err)
	}

	// Check that we got files
	if result.FileCount == 0 {
		t.Error("Expected to find files in testdata/sample")
	}

	// Verify deterministic hash
	result2, err := calc.CalculateDirectory(ctx, "testdata/sample", nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() second call error = %v", err)
	}

	if result.FileCount != result2.FileCount {
		t.Error("FileCount should be deterministic")
	}

	// Check file order is consistent
	if len(result.Files) != len(result2.Files) {
		t.Error("File count mismatch")
	}

	for i := range result.Files {
		if result.Files[i].Path != result2.Files[i].Path {
			t.Errorf("File order mismatch at index %d", i)
		}
		if result.Files[i].Hash != result2.Files[i].Hash {
			t.Errorf("File hash mismatch for %s", result.Files[i].Path)
		}
	}
}

func TestCalculateDirectoryWithPatterns(t *testing.T) {
	calc := NewCalculator(0)

	tests := []struct {
		name         string
		excludes     []string
		expectFiles  []string
		excludeFiles []string
	}{
		{
			name:     "no excludes - all files",
			excludes: nil,
			expectFiles: []string{
				"index.php",
				"config.php",
				"README.md",
				"script.js",
				"app.log",
				"error.log",
				"src/main.go",
				"src/lib/helper.go",
			},
			excludeFiles: []string{},
		},
		{
			name:     "exclude log files",
			excludes: []string{"*.log"},
			expectFiles: []string{
				"index.php",
				"config.php",
				"README.md",
				"script.js",
				"src/main.go",
				"src/lib/helper.go",
			},
			excludeFiles: []string{
				"app.log",
				"error.log",
			},
		},
		{
			name:     "exclude src directory",
			excludes: []string{"src/**"},
			expectFiles: []string{
				"index.php",
				"config.php",
				"README.md",
				"script.js",
				"app.log",
				"error.log",
			},
			excludeFiles: []string{
				"src/main.go",
				"src/lib/helper.go",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := calc.CalculateDirectory(context.Background(), "testdata/patterns", tt.excludes)
			if err != nil {
				t.Fatalf("CalculateDirectory() error = %v", err)
			}

			// Build a map of found files
			foundFiles := make(map[string]bool)
			for _, f := range result.Files {
				foundFiles[f.Path] = true
			}

			// Check expected files are present
			for _, expectedFile := range tt.expectFiles {
				if !foundFiles[expectedFile] {
					t.Errorf("Expected file %s not found", expectedFile)
				}
			}

			// Check excluded files are not present
			for _, excludedFile := range tt.excludeFiles {
				if foundFiles[excludedFile] {
					t.Errorf("File %s should have been excluded", excludedFile)
				}
			}
		})
	}
}

func TestMatchExcludePatterns(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		excludes []string
		expected bool // true if should be excluded
	}{
		{
			name:     "no excludes",
			path:     "index.php",
			excludes: nil,
			expected: false,
		},
		{
			name:     "exclude exact filename",
			path:     "test.log",
			excludes: []string{"test.log"},
			expected: true,
		},
		{
			name:     "exclude wildcard extension",
			path:     "test.log",
			excludes: []string{"*.log"},
			expected: true,
		},
		{
			name:     "exclude subdirectory with **",
			path:     "vendor/lib/file.php",
			excludes: []string{"vendor/**"},
			expected: true,
		},
		{
			name:     "not excluded",
			path:     "test.php",
			excludes: []string{"*.log"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchExcludePatterns(tt.path, tt.excludes)
			if result != tt.expected {
				t.Errorf("matchExcludePatterns(%s) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestVerifyIntegrity(t *testing.T) {
	calc := NewCalculator(0)

	// Generate initial manifest
	manifest, err := calc.CalculateDirectory(context.Background(), "testdata/sample", nil)
	if err != nil {
		t.Fatalf("Failed to generate manifest: %v", err)
	}

	// Verify against same directory should pass
	err = VerifyIntegrity(context.Background(), manifest, "testdata/sample")
	if err != nil {
		t.Errorf("VerifyIntegrity() should pass for unchanged files: %v", err)
	}

	// Create a temporary modified directory
	tempDir, err := os.MkdirTemp("", "test-verify")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Copy testdata files to temp directory
	copyTestData(t, "testdata/sample", tempDir)

	// Verify copied files should pass
	err = VerifyIntegrity(context.Background(), manifest, tempDir)
	if err != nil {
		t.Errorf("VerifyIntegrity() should pass for copied files: %v", err)
	}

	// Modify a file
	modifiedFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(modifiedFile, []byte("modified content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify should fail
	err = VerifyIntegrity(context.Background(), manifest, tempDir)
	if err == nil {
		t.Error("VerifyIntegrity() should fail for modified files")
	}

	// Check error message contains the modified file
	if err != nil && !containsString(err.Error(), "modified:") {
		t.Errorf("Error should mention modified files: %v", err)
	}
}

func TestVerifyIntegrityWithPatterns(t *testing.T) {
	calc := NewCalculator(0)

	// Create test directory
	tempDir, err := os.MkdirTemp("", "test-verify-patterns")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files
	files := map[string]string{
		"app.txt":    "app content",
		"config.txt": "config content",
		"debug.log":  "debug log content",
		"error.log":  "error log content",
		"cache.tmp":  "cache content",
	}

	for name, content := range files {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Test cases
	tests := []struct {
		name          string
		excludes      []string
		modifyFile    string
		addFile       string
		expectSuccess bool
		expectError   string
	}{
		{
			name:          "verify with excludes - no changes",
			excludes:      []string{"*.log", "*.tmp"},
			expectSuccess: true,
		},
		{
			name:          "verify with excludes - modified excluded file should pass",
			excludes:      []string{"*.log", "*.tmp"},
			modifyFile:    "debug.log",
			expectSuccess: true,
		},
		{
			name:          "verify with excludes - modified included file should fail",
			excludes:      []string{"*.log", "*.tmp"},
			modifyFile:    "app.txt",
			expectSuccess: false,
			expectError:   "modified: app.txt",
		},
		{
			name:          "verify with excludes - added excluded file should pass",
			excludes:      []string{"*.log", "*.tmp"},
			addFile:       "new.log",
			expectSuccess: true,
		},
		{
			name:          "verify with excludes - added included file should fail",
			excludes:      []string{"*.log", "*.tmp"},
			addFile:       "new.txt",
			expectSuccess: false,
			expectError:   "added: new.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fresh temp directory for each test
			testDir, err := os.MkdirTemp("", "test-verify-subtest")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(testDir)

			// Copy test files
			for name, content := range files {
				path := filepath.Join(testDir, name)
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}

			// Calculate manifest with excludes
			manifest, err := calc.CalculateDirectory(context.Background(), testDir, tt.excludes)
			if err != nil {
				t.Fatalf("CalculateDirectory() error = %v", err)
			}

			// Modify files if needed
			if tt.modifyFile != "" {
				path := filepath.Join(testDir, tt.modifyFile)
				if err := os.WriteFile(path, []byte("modified content"), 0644); err != nil {
					t.Fatal(err)
				}
			}

			// Add files if needed
			if tt.addFile != "" {
				path := filepath.Join(testDir, tt.addFile)
				if err := os.WriteFile(path, []byte("new file content"), 0644); err != nil {
					t.Fatal(err)
				}
			}

			// Verify with patterns
			err = VerifyIntegrityWithPatterns(context.Background(), manifest, testDir, tt.excludes)

			if tt.expectSuccess {
				if err != nil {
					t.Errorf("VerifyIntegrityWithPatterns() error = %v, want no error", err)
				}
			} else {
				if err == nil {
					t.Error("VerifyIntegrityWithPatterns() should fail but succeeded")
				} else if tt.expectError != "" && !containsString(err.Error(), tt.expectError) {
					t.Errorf("Error should contain '%s', got: %v", tt.expectError, err)
				}
			}
		})
	}
}

func TestSymlinkSecurity(t *testing.T) {
	calc := NewCalculator(0)

	t.Run("detect symlink manipulation", func(t *testing.T) {
		// Create test directory structure
		tempDir, err := os.MkdirTemp("", "test-symlink-security")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tempDir)

		// Create a regular file
		regularFile := filepath.Join(tempDir, "regular.txt")
		if err := os.WriteFile(regularFile, []byte("regular content"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create a symlink
		symlinkFile := filepath.Join(tempDir, "symlink.txt")
		targetFile := filepath.Join(tempDir, "target.txt")
		if err := os.WriteFile(targetFile, []byte("target content"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(targetFile, symlinkFile); err != nil {
			t.Fatal(err)
		}

		// Generate manifest
		manifest, err := calc.CalculateDirectory(context.Background(), tempDir, nil)
		if err != nil {
			t.Fatalf("Failed to generate manifest: %v", err)
		}

		// Verify we captured both regular file and symlink
		if manifest.FileCount != 3 { // regular.txt, target.txt, and symlink.txt
			t.Errorf("Expected 3 files, got %d", manifest.FileCount)
		}

		// Find the symlink in the manifest
		var symlinkInfo *FileInfo
		for i := range manifest.Files {
			if manifest.Files[i].Path == "symlink.txt" {
				symlinkInfo = &manifest.Files[i]
				break
			}
		}

		if symlinkInfo == nil {
			t.Fatal("Symlink not found in manifest")
		}

		if !symlinkInfo.IsSymlink {
			t.Error("Symlink should be marked as symlink")
		}

		if symlinkInfo.LinkTarget != targetFile {
			t.Errorf("Symlink target mismatch: expected %s, got %s", targetFile, symlinkInfo.LinkTarget)
		}

		// Test 1: Change symlink target - should be detected
		os.Remove(symlinkFile)
		newTarget := filepath.Join(tempDir, "evil.txt")
		if err := os.WriteFile(newTarget, []byte("evil content"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(newTarget, symlinkFile); err != nil {
			t.Fatal(err)
		}

		err = VerifyIntegrity(context.Background(), manifest, tempDir)
		if err == nil {
			t.Error("Should detect symlink target change")
		} else if !strings.Contains(err.Error(), "modified") {
			t.Errorf("Error should mention modification: %v", err)
		}

		// Test 2: Replace symlink with regular file - should be detected
		os.Remove(symlinkFile)
		if err := os.WriteFile(symlinkFile, []byte("now regular"), 0644); err != nil {
			t.Fatal(err)
		}

		err = VerifyIntegrity(context.Background(), manifest, tempDir)
		if err == nil {
			t.Error("Should detect symlink replaced with regular file")
		} else if !strings.Contains(err.Error(), "modified") {
			t.Errorf("Error should mention modification: %v", err)
		}

		// Test 3: Replace regular file with symlink - should be detected
		os.Remove(regularFile)
		if err := os.Symlink(targetFile, regularFile); err != nil {
			t.Fatal(err)
		}

		err = VerifyIntegrity(context.Background(), manifest, tempDir)
		if err == nil {
			t.Error("Should detect regular file replaced with symlink")
		} else if !strings.Contains(err.Error(), "modified") {
			t.Errorf("Error should mention modification: %v", err)
		}
	})
}

func TestSymlinkHandling(t *testing.T) {
	calc := NewCalculator(0)

	t.Run("directory symlink as target", func(t *testing.T) {
		// Calculate hash for the real directory
		realResult, err := calc.CalculateDirectory(context.Background(), "testdata/patterns", nil)
		if err != nil {
			t.Fatalf("Failed to calculate hash for real directory: %v", err)
		}

		// Calculate hash for the symlink to the directory
		symlinkResult, err := calc.CalculateDirectory(context.Background(), "testdata/symlink-to-patterns", nil)
		if err != nil {
			t.Fatalf("Failed to calculate hash for symlink directory: %v", err)
		}

		// Both should produce the same file count
		if realResult.FileCount != symlinkResult.FileCount {
			t.Errorf("File count mismatch: real=%d, symlink=%d", realResult.FileCount, symlinkResult.FileCount)
		}
	})

	t.Run("verify with directory symlink", func(t *testing.T) {
		// Generate manifest from real directory
		manifest, err := calc.CalculateDirectory(context.Background(), "testdata/patterns", nil)
		if err != nil {
			t.Fatalf("Failed to generate manifest: %v", err)
		}

		// Verify using symlink should pass
		err = VerifyIntegrity(context.Background(), manifest, "testdata/symlink-to-patterns")
		if err != nil {
			t.Errorf("Verification should pass for symlink: %v", err)
		}

		// Generate manifest from symlink
		symlinkManifest, err := calc.CalculateDirectory(context.Background(), "testdata/symlink-to-patterns", nil)
		if err != nil {
			t.Fatalf("Failed to generate manifest from symlink: %v", err)
		}

		// Verify using real directory should pass
		err = VerifyIntegrity(context.Background(), symlinkManifest, "testdata/patterns")
		if err != nil {
			t.Errorf("Verification should pass for real directory: %v", err)
		}
	})

	t.Run("file symlinks are tracked", func(t *testing.T) {
		// Calculate hash for directory containing file symlinks
		result, err := calc.CalculateDirectory(context.Background(), "testdata/symlink-test", nil)
		if err != nil {
			t.Fatalf("Failed to calculate hash: %v", err)
		}

		// Should find both the real file and the symlink
		if result.FileCount != 2 {
			t.Errorf("Expected 2 files (real.txt and link.txt), got %d", result.FileCount)
		}

		// Check that both files are included
		foundReal := false
		foundLink := false
		for _, f := range result.Files {
			if f.Path == "real.txt" {
				foundReal = true
				if f.IsSymlink {
					t.Error("real.txt should not be marked as symlink")
				}
			} else if f.Path == "link.txt" {
				foundLink = true
				if !f.IsSymlink {
					t.Error("link.txt should be marked as symlink")
				}
				if f.LinkTarget != "real.txt" {
					t.Errorf("link.txt should point to real.txt, got: %s", f.LinkTarget)
				}
			}
		}

		if !foundReal {
			t.Error("real.txt not found in results")
		}
		if !foundLink {
			t.Error("link.txt not found in results")
		}
	})
}

func TestParallelCalculation(t *testing.T) {
	calc := NewCalculator(0)
	calc.numWorkers = 4 // Use multiple workers

	// Create a directory with multiple files
	tempDir, err := os.MkdirTemp("", "test-parallel")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create 100 files
	for i := 0; i < 100; i++ {
		filename := filepath.Join(tempDir, fmt.Sprintf("file%03d.txt", i))
		content := fmt.Sprintf("content of file %d", i)
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Calculate hash
	result, err := calc.CalculateDirectory(context.Background(), tempDir, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() error = %v", err)
	}

	if result.FileCount != 100 {
		t.Errorf("Expected 100 files, got %d", result.FileCount)
	}

	// Verify deterministic with parallel processing
	result2, err := calc.CalculateDirectory(context.Background(), tempDir, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() second call error = %v", err)
	}

	if result.FileCount != result2.FileCount {
		t.Error("Parallel processing should produce deterministic results")
	}
}

func TestRateLimitedCalculation(t *testing.T) {
	// Create test directory with files
	tempDir, err := os.MkdirTemp("", "test-rate-limit")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create a few files
	testFiles := map[string]string{
		"small.txt":  "small content",
		"medium.txt": strings.Repeat("medium content ", 100),
		"large.txt":  strings.Repeat("large content ", 1000),
	}

	for filename, content := range testFiles {
		path := filepath.Join(tempDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Test without rate limit
	calcNormal := NewCalculator(2)
	result1, err := calcNormal.CalculateDirectory(context.Background(), tempDir, nil)
	if err != nil {
		t.Fatalf("Normal calculation failed: %v", err)
	}

	// Test with rate limit (1MB/s)
	calcRateLimit := NewCalculatorWithRateLimit(2, 1024*1024)
	result2, err := calcRateLimit.CalculateDirectory(context.Background(), tempDir, nil)
	if err != nil {
		t.Fatalf("Rate limited calculation failed: %v", err)
	}

	// Results should be identical

	if result1.FileCount != result2.FileCount {
		t.Errorf("File count mismatch: %d vs %d", result1.FileCount, result2.FileCount)
	}
}

func TestNewCalculatorWithRateLimit(t *testing.T) {
	tests := []struct {
		name          string
		numWorkers    int
		bytesPerSec   int64
		expectWorkers int
		expectLimit   bool
	}{
		{
			name:          "with rate limit",
			numWorkers:    4,
			bytesPerSec:   1024 * 1024, // 1MB/s
			expectWorkers: 4,
			expectLimit:   true,
		},
		{
			name:          "no rate limit",
			numWorkers:    2,
			bytesPerSec:   0,
			expectWorkers: 2,
			expectLimit:   false,
		},
		{
			name:          "auto workers with rate limit",
			numWorkers:    0,
			bytesPerSec:   512 * 1024, // 512KB/s
			expectWorkers: runtime.GOMAXPROCS(0),
			expectLimit:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := NewCalculatorWithRateLimit(tt.numWorkers, tt.bytesPerSec)

			if calc.numWorkers != tt.expectWorkers {
				t.Errorf("numWorkers = %d, want %d", calc.numWorkers, tt.expectWorkers)
			}

			if calc.bytesPerSec != tt.bytesPerSec {
				t.Errorf("bytesPerSec = %d, want %d", calc.bytesPerSec, tt.bytesPerSec)
			}

			if tt.expectLimit && calc.limiter == nil {
				t.Error("Expected limiter to be created")
			}

			if !tt.expectLimit && calc.limiter != nil {
				t.Error("Expected no limiter")
			}
		})
	}
}

// Helper functions

func copyTestData(t *testing.T, src, dst string) {
	t.Helper()

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})

	if err != nil {
		t.Fatal(err)
	}
}

func containsString(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				len(s) > len(substr) && containsString(s[1:len(s)-1], substr)))
}
