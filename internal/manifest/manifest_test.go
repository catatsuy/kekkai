package manifest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/catatsuy/kekkai/internal/hash"
)

func TestGenerateManifest(t *testing.T) {
	// Create test directory structure
	tempDir := createTestDirectory(t)
	defer os.RemoveAll(tempDir)

	generator := NewGenerator()
	manifest, err := generator.Generate(tempDir, nil, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Check manifest fields
	if manifest.Version != "1.0" {
		t.Errorf("Version = %v, want 1.0", manifest.Version)
	}

	if manifest.TotalHash == "" {
		t.Error("TotalHash should not be empty")
	}

	if manifest.FileCount == 0 {
		t.Error("FileCount should be greater than 0")
	}

	if len(manifest.Files) != manifest.FileCount {
		t.Errorf("Files length = %d, want %d", len(manifest.Files), manifest.FileCount)
	}

	// Check timestamp format
	_, err = time.Parse(time.RFC3339, manifest.GeneratedAt)
	if err != nil {
		t.Errorf("GeneratedAt format error: %v", err)
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	// Create a test manifest
	manifest := &Manifest{
		Version:     "1.0",
		TotalHash:   "abc123def456",
		FileCount:   2,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Files: []hash.FileInfo{
			{Path: "file1.txt", Hash: "hash1", Size: 100},
			{Path: "file2.txt", Hash: "hash2", Size: 200},
		},
	}

	// Test SaveToFile and LoadFromFile
	t.Run("file operations", func(t *testing.T) {
		tempFile := filepath.Join(t.TempDir(), "manifest.json")

		// Save
		err := SaveToFile(manifest, tempFile)
		if err != nil {
			t.Fatalf("SaveToFile() error = %v", err)
		}

		// Load
		loaded, err := LoadFromFile(tempFile)
		if err != nil {
			t.Fatalf("LoadFromFile() error = %v", err)
		}

		// Compare
		if loaded.TotalHash != manifest.TotalHash {
			t.Errorf("TotalHash = %v, want %v", loaded.TotalHash, manifest.TotalHash)
		}

		if loaded.FileCount != manifest.FileCount {
			t.Errorf("FileCount = %d, want %d", loaded.FileCount, manifest.FileCount)
		}

		if len(loaded.Files) != len(manifest.Files) {
			t.Errorf("Files length = %d, want %d", len(loaded.Files), len(manifest.Files))
		}
	})

	// Test SaveToWriter and LoadFromReader
	t.Run("writer/reader operations", func(t *testing.T) {
		var buf bytes.Buffer

		// Save to writer
		err := SaveToWriter(manifest, &buf)
		if err != nil {
			t.Fatalf("SaveToWriter() error = %v", err)
		}

		// Load from reader
		loaded, err := LoadFromReader(&buf)
		if err != nil {
			t.Fatalf("LoadFromReader() error = %v", err)
		}

		// Compare
		if loaded.TotalHash != manifest.TotalHash {
			t.Errorf("TotalHash = %v, want %v", loaded.TotalHash, manifest.TotalHash)
		}
	})
}

func TestManifestJSON(t *testing.T) {
	manifest := &Manifest{
		Version:     "1.0",
		TotalHash:   "test-hash",
		FileCount:   1,
		GeneratedAt: "2024-01-01T00:00:00Z",
		Files: []hash.FileInfo{
			{Path: "test.txt", Hash: "file-hash", Size: 42},
		},
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Check JSON structure
	jsonStr := string(data)
	expectedFields := []string{
		`"version": "1.0"`,
		`"total_hash": "test-hash"`,
		`"file_count": 1`,
		`"generated_at": "2024-01-01T00:00:00Z"`,
		`"path": "test.txt"`,
		`"hash": "file-hash"`,
		`"size": 42`,
	}

	for _, field := range expectedFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("JSON should contain %s", field)
		}
	}

	// Unmarshal back
	var loaded Manifest
	err = json.Unmarshal(data, &loaded)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if loaded.TotalHash != manifest.TotalHash {
		t.Errorf("Unmarshaled TotalHash = %v, want %v", loaded.TotalHash, manifest.TotalHash)
	}
}

func TestManifestVerify(t *testing.T) {
	// Create test directory
	tempDir := createTestDirectory(t)
	defer os.RemoveAll(tempDir)

	// Generate manifest
	generator := NewGenerator()
	manifest, err := generator.Generate(tempDir, nil, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Verify should pass
	err = manifest.Verify(tempDir)
	if err != nil {
		t.Errorf("Verify() should pass for unchanged files: %v", err)
	}

	// Modify a file
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("modified"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Verify should fail
	err = manifest.Verify(tempDir)
	if err == nil {
		t.Error("Verify() should fail for modified files")
	}
}

func TestGetSummary(t *testing.T) {
	manifest := &Manifest{
		Version:     "1.0",
		TotalHash:   "abc123",
		FileCount:   10,
		GeneratedAt: "2024-01-01T00:00:00Z",
	}

	summary := manifest.GetSummary()

	expectedParts := []string{
		"Version: 1.0",
		"Generated: 2024-01-01T00:00:00Z",
		"Total Hash: abc123",
		"File Count: 10",
	}

	for _, part := range expectedParts {
		if !strings.Contains(summary, part) {
			t.Errorf("Summary should contain '%s'", part)
		}
	}
}

func TestManifestWithPatterns(t *testing.T) {
	// Create test directory with various file types
	tempDir := t.TempDir()

	// Create files
	files := map[string]string{
		"index.php":      "<?php echo 'hello';",
		"config.php":     "<?php return [];",
		"README.md":      "# README",
		"script.js":      "console.log('test');",
		"style.css":      "body { margin: 0; }",
		"error.log":      "error message",
		"vendor/lib.php": "<?php // vendor",
		"cache/temp.txt": "cached",
		"src/main.go":    "package main",
	}

	for path, content := range files {
		fullPath := filepath.Join(tempDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name        string
		includes    []string
		excludes    []string
		expectCount int
	}{
		{
			name:        "all files",
			includes:    nil,
			excludes:    nil,
			expectCount: len(files),
		},
		{
			name:        "only PHP files",
			includes:    []string{"*.php", "**/*.php"},
			excludes:    nil,
			expectCount: 3, // index.php, config.php, vendor/lib.php
		},
		{
			name:        "exclude logs and cache",
			includes:    nil,
			excludes:    []string{"*.log", "cache/**"},
			expectCount: len(files) - 2,
		},
		{
			name:        "PHP files excluding vendor",
			includes:    []string{"*.php"},
			excludes:    []string{"vendor/**"},
			expectCount: 2, // index.php, config.php
		},
	}

	generator := NewGenerator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := generator.Generate(tempDir, tt.includes, tt.excludes)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			if manifest.FileCount != tt.expectCount {
				t.Errorf("FileCount = %d, want %d", manifest.FileCount, tt.expectCount)
			}
		})
	}
}

func TestManifestExcludePatterns(t *testing.T) {
	// Create test directory
	tempDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"app.go":      "package main",
		"config.json": `{"key": "value"}`,
		"debug.log":   "debug messages",
		"error.log":   "error messages",
		".env":        "SECRET=value",
		"README.md":   "# Project",
	}

	for name, content := range files {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Generate manifest with excludes
	generator := NewGenerator()
	excludes := []string{"*.log", ".env"}
	manifest, err := generator.Generate(tempDir, nil, excludes)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Check excludes are saved in manifest
	if !reflect.DeepEqual(manifest.Excludes, excludes) {
		t.Errorf("Manifest.Excludes = %v, want %v", manifest.Excludes, excludes)
	}

	// Check that excluded files are not in the manifest
	for _, file := range manifest.Files {
		if strings.HasSuffix(file.Path, ".log") || file.Path == ".env" {
			t.Errorf("Excluded file %s should not be in manifest", file.Path)
		}
	}

	// Test manifest.Verify() uses excludes correctly
	t.Run("verify with original files", func(t *testing.T) {
		err := manifest.Verify(tempDir)
		if err != nil {
			t.Errorf("Verify() should succeed with original files: %v", err)
		}
	})

	t.Run("verify with modified excluded file", func(t *testing.T) {
		// Modify an excluded file
		logPath := filepath.Join(tempDir, "debug.log")
		if err := os.WriteFile(logPath, []byte("modified debug log"), 0644); err != nil {
			t.Fatal(err)
		}

		// Verify should still pass because the file is excluded
		err := manifest.Verify(tempDir)
		if err != nil {
			t.Errorf("Verify() should succeed even with modified excluded file: %v", err)
		}
	})

	t.Run("verify with added excluded file", func(t *testing.T) {
		// Add a new log file (which should be excluded)
		newLogPath := filepath.Join(tempDir, "new.log")
		if err := os.WriteFile(newLogPath, []byte("new log file"), 0644); err != nil {
			t.Fatal(err)
		}

		// Verify should still pass because the file matches exclude pattern
		err := manifest.Verify(tempDir)
		if err != nil {
			t.Errorf("Verify() should succeed even with added excluded file: %v", err)
		}
	})

	t.Run("verify with modified included file", func(t *testing.T) {
		// Modify an included file
		appPath := filepath.Join(tempDir, "app.go")
		if err := os.WriteFile(appPath, []byte("package modified"), 0644); err != nil {
			t.Fatal(err)
		}

		// Verify should fail because the file is included
		err := manifest.Verify(tempDir)
		if err == nil {
			t.Error("Verify() should fail with modified included file")
		} else if !strings.Contains(err.Error(), "modified: app.go") {
			t.Errorf("Error should mention modified file, got: %v", err)
		}

		// Restore the file
		if err := os.WriteFile(appPath, []byte("package main"), 0644); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("verify with added included file", func(t *testing.T) {
		// Add a new go file (which should be included)
		newGoPath := filepath.Join(tempDir, "new.go")
		if err := os.WriteFile(newGoPath, []byte("package new"), 0644); err != nil {
			t.Fatal(err)
		}

		// Verify should fail because the file is not excluded
		err := manifest.Verify(tempDir)
		if err == nil {
			t.Error("Verify() should fail with added included file")
		} else if !strings.Contains(err.Error(), "added: new.go") {
			t.Errorf("Error should mention added file, got: %v", err)
		}

		// Clean up
		os.Remove(newGoPath)
	})
}

// Helper function to create a test directory with files
func createTestDirectory(t *testing.T) string {
	t.Helper()

	tempDir := t.TempDir()

	// Create some test files
	files := map[string]string{
		"test.txt":   "test content",
		"index.html": "<html></html>",
		"script.js":  "console.log('test');",
	}

	for name, content := range files {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return tempDir
}
