package hash

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

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
			calc := NewCalculator()
			hash, err := calc.hashFile(tmpfile.Name())
			if err != nil {
				t.Fatalf("hashFile() error = %v", err)
			}

			if hash != tt.expected {
				t.Errorf("hashFile() = %v, want %v", hash, tt.expected)
			}
		})
	}
}

func TestCalculateDirectory(t *testing.T) {
	calc := NewCalculator()

	// Test with testdata directory
	result, err := calc.CalculateDirectory("testdata/sample", nil, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() error = %v", err)
	}

	// Check that we got files
	if result.FileCount == 0 {
		t.Error("Expected to find files in testdata/sample")
	}

	// Verify deterministic hash
	result2, err := calc.CalculateDirectory("testdata/sample", nil, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() second call error = %v", err)
	}

	if result.TotalHash != result2.TotalHash {
		t.Error("TotalHash should be deterministic")
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
	calc := NewCalculator()

	tests := []struct {
		name         string
		includes     []string
		excludes     []string
		expectFiles  []string
		excludeFiles []string
	}{
		{
			name:     "include PHP files",
			includes: []string{"*.php"},
			excludes: nil,
			expectFiles: []string{
				"index.php",
				"config.php",
			},
			excludeFiles: []string{
				"README.md",
				"script.js",
			},
		},
		{
			name:     "exclude log files",
			includes: nil,
			excludes: []string{"*.log"},
			expectFiles: []string{
				"index.php",
				"README.md",
			},
			excludeFiles: []string{
				"app.log",
				"error.log",
			},
		},
		{
			name:     "include subdirectories",
			includes: []string{"src/**"},
			excludes: nil,
			expectFiles: []string{
				"src/main.go",
				"src/lib/helper.go",
			},
			excludeFiles: []string{
				"index.php",
				"README.md",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := calc.CalculateDirectory("testdata/patterns", tt.includes, tt.excludes)
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

func TestMatchPatterns(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		includes []string
		excludes []string
		expected bool
	}{
		{
			name:     "match exact filename",
			path:     "index.php",
			includes: []string{"index.php"},
			excludes: nil,
			expected: true,
		},
		{
			name:     "match wildcard extension",
			path:     "test.php",
			includes: []string{"*.php"},
			excludes: nil,
			expected: true,
		},
		{
			name:     "exclude pattern",
			path:     "test.log",
			includes: []string{"*"},
			excludes: []string{"*.log"},
			expected: false,
		},
		{
			name:     "subdirectory with **",
			path:     "src/main.go",
			includes: []string{"src/**"},
			excludes: nil,
			expected: true,
		},
		{
			name:     "deep subdirectory with **",
			path:     "vendor/lib/deep/file.php",
			includes: []string{"vendor/**"},
			excludes: nil,
			expected: true,
		},
		{
			name:     "exclude overrides include",
			path:     "test.php",
			includes: []string{"*.php"},
			excludes: []string{"test.php"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchPatterns(tt.path, tt.includes, tt.excludes)
			if result != tt.expected {
				t.Errorf("matchPatterns(%s) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestVerifyIntegrity(t *testing.T) {
	calc := NewCalculator()

	// Generate initial manifest
	manifest, err := calc.CalculateDirectory("testdata/sample", nil, nil)
	if err != nil {
		t.Fatalf("Failed to generate manifest: %v", err)
	}

	// Verify against same directory should pass
	err = VerifyIntegrity(manifest, "testdata/sample")
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
	err = VerifyIntegrity(manifest, tempDir)
	if err != nil {
		t.Errorf("VerifyIntegrity() should pass for copied files: %v", err)
	}

	// Modify a file
	modifiedFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(modifiedFile, []byte("modified content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify should fail
	err = VerifyIntegrity(manifest, tempDir)
	if err == nil {
		t.Error("VerifyIntegrity() should fail for modified files")
	}

	// Check error message contains the modified file
	if err != nil && !containsString(err.Error(), "modified:") {
		t.Errorf("Error should mention modified files: %v", err)
	}
}

func TestVerifyIntegrityWithPatterns(t *testing.T) {
	calc := NewCalculator()

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
			manifest, err := calc.CalculateDirectory(testDir, nil, tt.excludes)
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
			err = VerifyIntegrityWithPatterns(manifest, testDir, nil, tt.excludes)

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

func TestSymlinkHandling(t *testing.T) {
	calc := NewCalculator()

	t.Run("directory symlink as target", func(t *testing.T) {
		// Calculate hash for the real directory
		realResult, err := calc.CalculateDirectory("testdata/patterns", nil, nil)
		if err != nil {
			t.Fatalf("Failed to calculate hash for real directory: %v", err)
		}

		// Calculate hash for the symlink to the directory
		symlinkResult, err := calc.CalculateDirectory("testdata/symlink-to-patterns", nil, nil)
		if err != nil {
			t.Fatalf("Failed to calculate hash for symlink directory: %v", err)
		}

		// Both should produce the same hash
		if realResult.TotalHash != symlinkResult.TotalHash {
			t.Error("Hash for real directory and symlink should be identical")
		}

		if realResult.FileCount != symlinkResult.FileCount {
			t.Errorf("File count mismatch: real=%d, symlink=%d", realResult.FileCount, symlinkResult.FileCount)
		}
	})

	t.Run("verify with directory symlink", func(t *testing.T) {
		// Generate manifest from real directory
		manifest, err := calc.CalculateDirectory("testdata/patterns", nil, nil)
		if err != nil {
			t.Fatalf("Failed to generate manifest: %v", err)
		}

		// Verify using symlink should pass
		err = VerifyIntegrity(manifest, "testdata/symlink-to-patterns")
		if err != nil {
			t.Errorf("Verification should pass for symlink: %v", err)
		}

		// Generate manifest from symlink
		symlinkManifest, err := calc.CalculateDirectory("testdata/symlink-to-patterns", nil, nil)
		if err != nil {
			t.Fatalf("Failed to generate manifest from symlink: %v", err)
		}

		// Verify using real directory should pass
		err = VerifyIntegrity(symlinkManifest, "testdata/patterns")
		if err != nil {
			t.Errorf("Verification should pass for real directory: %v", err)
		}
	})

	t.Run("file symlinks are skipped", func(t *testing.T) {
		// Calculate hash for directory containing file symlinks
		result, err := calc.CalculateDirectory("testdata/symlink-test", nil, nil)
		if err != nil {
			t.Fatalf("Failed to calculate hash: %v", err)
		}

		// Should only find the real file, not the symlink
		if result.FileCount != 1 {
			t.Errorf("Expected 1 file (real.txt), got %d", result.FileCount)
		}

		// Check that only real.txt is included
		if len(result.Files) != 1 || result.Files[0].Path != "real.txt" {
			t.Errorf("Expected only real.txt, got: %v", result.Files)
		}
	})
}

func TestParallelCalculation(t *testing.T) {
	calc := NewCalculator()
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
	result, err := calc.CalculateDirectory(tempDir, nil, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() error = %v", err)
	}

	if result.FileCount != 100 {
		t.Errorf("Expected 100 files, got %d", result.FileCount)
	}

	// Verify deterministic with parallel processing
	result2, err := calc.CalculateDirectory(tempDir, nil, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() second call error = %v", err)
	}

	if result.TotalHash != result2.TotalHash {
		t.Error("Parallel processing should produce deterministic results")
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
