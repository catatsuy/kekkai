package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestVerifier(t *testing.T, cacheDir, baseName, appName string) *MetadataVerifier {
	t.Helper()
	verifier, err := NewMetadataVerifier(cacheDir, baseName, appName)
	if err != nil {
		t.Fatalf("NewMetadataVerifier() returned error: %v", err)
	}
	return verifier
}

func TestMetadataVerifier_NewAndLoad(t *testing.T) {
	// Create temporary directory for cache
	tempDir := t.TempDir()

	// Create new verifier
	verifier := newTestVerifier(t, tempDir, "test", "app")

	// Load should work even if file doesn't exist (creates empty cache)
	err := verifier.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Data should be initialized
	if verifier.data == nil {
		t.Fatal("Cache data should be initialized")
	}

	if verifier.data.Version != "2.0" {
		t.Errorf("Expected version 2.0, got %s", verifier.data.Version)
	}
}

func TestMetadataVerifier_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()

	// Create verifier and load
	verifier := newTestVerifier(t, tempDir, "test", "app")
	err := verifier.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Set manifest time
	manifestTime := time.Now().Add(-1 * time.Hour)
	verifier.SetManifestTime(manifestTime)

	// Save cache
	err = verifier.Save()
	if err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Create new verifier and load saved data
	verifier2 := newTestVerifier(t, tempDir, "test", "app")
	err = verifier2.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Check data was loaded correctly
	if verifier2.data.Version != "2.0" {
		t.Errorf("Expected version 2.0, got %s", verifier2.data.Version)
	}

	if !verifier2.data.ManifestGenTime.Equal(manifestTime) {
		t.Errorf("Manifest time not preserved")
	}

	// Verify cache integrity
	if verifier2.data.CacheHash == "" {
		t.Error("Cache hash should be set")
	}
}

func TestMetadataVerifier_UpdateAndCheckMetadata(t *testing.T) {
	tempDir := t.TempDir()
	targetDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(targetDir, "test.txt")
	content := []byte("test content")
	err := os.WriteFile(testFile, content, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create verifier
	verifier := newTestVerifier(t, tempDir, "test", "app")
	err = verifier.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Initially, file should not be in cache
	if verifier.CheckMetadata(testFile) {
		t.Error("File should not be in cache initially")
	}

	// Update metadata
	err = verifier.UpdateMetadata(testFile)
	if err != nil {
		t.Fatalf("UpdateMetadata() failed: %v", err)
	}

	// Now file should be in cache and match
	if !verifier.CheckMetadata(testFile) {
		t.Error("File should be in cache and match")
	}

	// Modify file
	newContent := []byte("modified content")
	err = os.WriteFile(testFile, newContent, 0644)
	if err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Sleep to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)

	// File should no longer match cache
	if verifier.CheckMetadata(testFile) {
		t.Error("File should not match cache after modification")
	}
}

func TestMetadataVerifier_ManifestValidity(t *testing.T) {
	tempDir := t.TempDir()

	verifier := newTestVerifier(t, tempDir, "test", "app")
	err := verifier.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	// Set manifest time to past
	verifier.SetManifestTime(past)

	// Cache created now should be valid for past manifest
	if !verifier.IsValidForManifest(past) {
		t.Error("Cache should be valid for past manifest")
	}

	// Cache should not be valid for future manifest
	if verifier.IsValidForManifest(future) {
		t.Error("Cache should not be valid for future manifest")
	}
}

func TestMetadataVerifier_Clear(t *testing.T) {
	tempDir := t.TempDir()
	targetDir := t.TempDir()

	// Create test file and update cache
	testFile := filepath.Join(targetDir, "test.txt")
	content := []byte("test content")
	err := os.WriteFile(testFile, content, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	verifier := newTestVerifier(t, tempDir, "test", "app")
	err = verifier.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	err = verifier.UpdateMetadata(testFile)
	if err != nil {
		t.Fatalf("UpdateMetadata() failed: %v", err)
	}

	// Verify file is in cache
	if !verifier.CheckMetadata(testFile) {
		t.Error("File should be in cache")
	}

	// Clear cache
	verifier.Clear()

	// File should no longer be in cache
	if verifier.CheckMetadata(testFile) {
		t.Error("File should not be in cache after clear")
	}

	// Cache should still be properly initialized
	if verifier.data == nil || verifier.data.Version != "2.0" {
		t.Error("Cache should be properly initialized after clear")
	}
}

func TestMetadataVerifier_CacheFilename(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		baseName     string
		appName      string
		expectedFile string
	}{
		{"development", "", ".kekkai-cache-development-.json"},
		{"production", "myapp", ".kekkai-cache-production-myapp.json"},
		{"staging", "webapp", ".kekkai-cache-staging-webapp.json"},
	}

	for _, tt := range tests {
		t.Run(tt.baseName+"-"+tt.appName, func(t *testing.T) {
			verifier := newTestVerifier(t, tempDir, tt.baseName, tt.appName)
			expectedPath := filepath.Join(tempDir, tt.expectedFile)

			if verifier.cachePath != expectedPath {
				t.Errorf("Expected cache path %s, got %s", expectedPath, verifier.cachePath)
			}
		})
	}
}

func TestMetadataVerifier_InvalidCacheDir(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "not_a_dir")
	if err := os.WriteFile(filePath, []byte("test"), 0600); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	if _, err := NewMetadataVerifier(filePath, "test", "app"); err == nil {
		t.Fatal("Expected error when cacheDir is a file")
	}
}

func TestMetadataVerifier_NonexistentCacheDir(t *testing.T) {
	tempDir := t.TempDir()
	nonexistent := filepath.Join(tempDir, "missing")
	if _, err := NewMetadataVerifier(nonexistent, "test", "app"); err == nil {
		t.Fatal("Expected error when cacheDir does not exist")
	}
}

func TestMetadataVerifier_EmptyCacheDirUsesTemp(t *testing.T) {
	verifier, err := NewMetadataVerifier("", "test", "app")
	if err != nil {
		t.Fatalf("NewMetadataVerifier returned error for empty cacheDir: %v", err)
	}
	if filepath.Clean(verifier.cacheDir) != filepath.Clean(os.TempDir()) {
		t.Fatalf("Expected cacheDir %q to equal os.TempDir() %q", verifier.cacheDir, os.TempDir())
	}
	if filepath.Clean(filepath.Dir(verifier.cachePath)) != filepath.Clean(os.TempDir()) {
		t.Fatalf("Expected cachePath directory %q to equal os.TempDir() %q", filepath.Dir(verifier.cachePath), os.TempDir())
	}
}

func TestMetadataVerifier_CacheIntegrity(t *testing.T) {
	tempDir := t.TempDir()

	verifier := newTestVerifier(t, tempDir, "test", "app")
	err := verifier.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Save valid cache
	err = verifier.Save()
	if err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Manually corrupt the cache file
	cacheFile := verifier.cachePath
	corruptedData := []byte(`{"version":"2.0","cache_hash":"invalid","files":{}}`)
	err = os.WriteFile(cacheFile, corruptedData, 0644)
	if err != nil {
		t.Fatalf("Failed to write corrupted cache: %v", err)
	}

	// Load should detect corruption and start fresh
	verifier2 := newTestVerifier(t, tempDir, "test", "app")
	err = verifier2.Load()
	if err == nil {
		t.Error("Load() should detect corrupted cache")
	}

	// Data should still be initialized (fresh cache)
	if verifier2.data == nil || verifier2.data.Version != "2.0" {
		t.Error("Cache should be initialized even after corruption")
	}
}

func TestMetadataVerifier_ConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	targetDir := t.TempDir()

	// Create test files
	var testFiles []string
	for i := 0; i < 10; i++ {
		testFile := filepath.Join(targetDir, "test"+string(rune('0'+i))+".txt")
		content := []byte("test content " + string(rune('0'+i)))
		err := os.WriteFile(testFile, content, 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		testFiles = append(testFiles, testFile)
	}

	verifier := newTestVerifier(t, tempDir, "test", "app")
	err := verifier.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Concurrent updates and checks
	done := make(chan bool, 20)

	// Start updaters
	for i := 0; i < 10; i++ {
		go func(file string) {
			defer func() { done <- true }()
			err := verifier.UpdateMetadata(file)
			if err != nil {
				t.Errorf("UpdateMetadata() failed: %v", err)
			}
		}(testFiles[i])
	}

	// Start checkers
	for i := 0; i < 10; i++ {
		go func(file string) {
			defer func() { done <- true }()
			// This shouldn't panic even if called concurrently
			_ = verifier.CheckMetadata(file)
		}(testFiles[i])
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}
