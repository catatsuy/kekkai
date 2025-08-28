package hash

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCalculator_WithMetadataCache(t *testing.T) {
	// Create temporary directories
	tempDir := t.TempDir()
	cacheDir := t.TempDir()

	// Create test files
	testFiles := map[string]string{
		"file1.txt":     "content1",
		"file2.txt":     "content2",
		"dir/file3.txt": "content3",
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(tempDir, path)
		err := os.MkdirAll(filepath.Dir(fullPath), 0755)
		if err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		err = os.WriteFile(fullPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Create calculator with cache
	calculator := NewCalculator(2)
	manifestTime := time.Now().Add(-1 * time.Hour)
	err := calculator.EnableMetadataCache(cacheDir, tempDir, "test", "app", manifestTime)
	if err != nil {
		t.Fatalf("EnableMetadataCache() failed: %v", err)
	}

	// Set verify probability to 1.0 to always calculate hash initially
	calculator.SetVerifyProbability(1.0)

	// First calculation - should calculate all hashes
	ctx := context.Background()
	result1, err := calculator.CalculateDirectory(ctx, tempDir, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() failed: %v", err)
	}

	if result1.FileCount != 3 {
		t.Errorf("Expected 3 files, got %d", result1.FileCount)
	}

	// Update cache for all files
	err = calculator.UpdateCacheForFiles(tempDir, result1.Files)
	if err != nil {
		t.Fatalf("UpdateCacheForFiles() failed: %v", err)
	}

	// Save cache
	err = calculator.SaveMetadataCache()
	if err != nil {
		t.Fatalf("SaveMetadataCache() failed: %v", err)
	}

	// Create new calculator with same cache
	calculator2 := NewCalculator(2)
	err = calculator2.EnableMetadataCache(cacheDir, tempDir, "test", "app", manifestTime)
	if err != nil {
		t.Fatalf("EnableMetadataCache() failed: %v", err)
	}

	// Set verify probability to 0.0 to always use cache
	calculator2.SetVerifyProbability(0.0)

	// Set manifest hashes for cache-based verification
	manifestHashes := make(map[string]string)
	for _, f := range result1.Files {
		manifestHashes[f.Path] = f.Hash
	}
	calculator2.SetManifestHashes(manifestHashes)

	// Second calculation - should use cache for metadata checks
	result2, err := calculator2.CalculateDirectory(ctx, tempDir, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() with cache failed: %v", err)
	}

	// Results should be identical

	if result1.FileCount != result2.FileCount {
		t.Errorf("File count mismatch: %d != %d", result1.FileCount, result2.FileCount)
	}
}

func TestCalculator_CacheWithFileModification(t *testing.T) {
	tempDir := t.TempDir()
	cacheDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tempDir, "test.txt")
	originalContent := []byte("original content")
	err := os.WriteFile(testFile, originalContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create calculator with cache
	calculator := NewCalculator(1)
	manifestTime := time.Now().Add(-1 * time.Hour)
	err = calculator.EnableMetadataCache(cacheDir, tempDir, "test", "app", manifestTime)
	if err != nil {
		t.Fatalf("EnableMetadataCache() failed: %v", err)
	}

	calculator.SetVerifyProbability(1.0) // Always calculate initially

	// First calculation
	ctx := context.Background()
	result1, err := calculator.CalculateDirectory(ctx, tempDir, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() failed: %v", err)
	}

	originalHash := result1.Files[0].Hash

	// Update cache
	err = calculator.UpdateCacheForFiles(tempDir, result1.Files)
	if err != nil {
		t.Fatalf("UpdateCacheForFiles() failed: %v", err)
	}

	err = calculator.SaveMetadataCache()
	if err != nil {
		t.Fatalf("SaveMetadataCache() failed: %v", err)
	}

	// Sleep first to ensure different timestamp
	time.Sleep(1 * time.Second)

	// Modify file content and permissions to ensure ctime changes
	modifiedContent := []byte("modified content that is significantly different from original")
	err = os.WriteFile(testFile, modifiedContent, 0644)
	if err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Change permissions to ensure ctime change on Linux
	err = os.Chmod(testFile, 0755)
	if err != nil {
		t.Fatalf("Failed to change file permissions: %v", err)
	}

	// Additional sleep to ensure timestamp changes are visible
	time.Sleep(100 * time.Millisecond)

	// Create new calculator
	calculator2 := NewCalculator(1)
	err = calculator2.EnableMetadataCache(cacheDir, tempDir, "test", "app", manifestTime)
	if err != nil {
		t.Fatalf("EnableMetadataCache() failed: %v", err)
	}

	calculator2.SetVerifyProbability(0.0) // Try to use cache

	// Set original manifest hashes
	manifestHashes := map[string]string{
		"test.txt": originalHash,
	}
	calculator2.SetManifestHashes(manifestHashes)

	// Second calculation - should detect file change and recalculate
	result2, err := calculator2.CalculateDirectory(ctx, tempDir, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() failed: %v", err)
	}

	newHash := result2.Files[0].Hash

	// Hash should be different due to content change
	if originalHash == newHash {
		t.Error("Hash should be different after file modification")
	}

	// Verify that files were recalculated due to cache invalidation
	if len(result2.Files) == 0 {
		t.Error("Should have calculated files after cache invalidation")
	}
}

func TestCalculator_ProbabilisticVerification(t *testing.T) {
	tempDir := t.TempDir()
	cacheDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tempDir, "test.txt")
	content := []byte("test content")
	err := os.WriteFile(testFile, content, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create calculator with cache
	calculator := NewCalculator(1)
	manifestTime := time.Now().Add(-1 * time.Hour)
	err = calculator.EnableMetadataCache(cacheDir, tempDir, "test", "app", manifestTime)
	if err != nil {
		t.Fatalf("EnableMetadataCache() failed: %v", err)
	}

	// First calculation with probability 1.0 (always calculate)
	calculator.SetVerifyProbability(1.0)

	ctx := context.Background()
	result1, err := calculator.CalculateDirectory(ctx, tempDir, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() failed: %v", err)
	}

	// Update cache
	err = calculator.UpdateCacheForFiles(tempDir, result1.Files)
	if err != nil {
		t.Fatalf("UpdateCacheForFiles() failed: %v", err)
	}

	err = calculator.SaveMetadataCache()
	if err != nil {
		t.Fatalf("SaveMetadataCache() failed: %v", err)
	}

	// Test different probabilities
	probabilities := []float64{0.0, 0.5, 1.0}

	for _, prob := range probabilities {
		t.Run(fmt.Sprintf("prob_%.1f", prob), func(t *testing.T) {
			calculator2 := NewCalculator(1)
			err = calculator2.EnableMetadataCache(cacheDir, tempDir, "test", "app", manifestTime)
			if err != nil {
				t.Fatalf("EnableMetadataCache() failed: %v", err)
			}

			calculator2.SetVerifyProbability(prob)

			// Set manifest hashes
			manifestHashes := map[string]string{
				"test.txt": result1.Files[0].Hash,
			}
			calculator2.SetManifestHashes(manifestHashes)

			// Calculate - should work regardless of probability
			result2, err := calculator2.CalculateDirectory(ctx, tempDir, nil)
			if err != nil {
				t.Fatalf("CalculateDirectory() failed with probability %f: %v", prob, err)
			}

			// Results should be consistent
			if result1.FileCount != result2.FileCount {
				t.Errorf("File count inconsistent with probability %f", prob)
			}
		})
	}
}

func TestCalculator_CacheInvalidation(t *testing.T) {
	tempDir := t.TempDir()
	cacheDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tempDir, "test.txt")
	content := []byte("test content")
	err := os.WriteFile(testFile, content, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create calculator with old manifest time
	oldManifestTime := time.Now().Add(-2 * time.Hour)

	calculator := NewCalculator(1)
	err = calculator.EnableMetadataCache(cacheDir, tempDir, "test", "app", oldManifestTime)
	if err != nil {
		t.Fatalf("EnableMetadataCache() failed: %v", err)
	}

	// Calculate and save cache
	ctx := context.Background()
	result1, err := calculator.CalculateDirectory(ctx, tempDir, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory() failed: %v", err)
	}

	err = calculator.UpdateCacheForFiles(tempDir, result1.Files)
	if err != nil {
		t.Fatalf("UpdateCacheForFiles() failed: %v", err)
	}

	err = calculator.SaveMetadataCache()
	if err != nil {
		t.Fatalf("SaveMetadataCache() failed: %v", err)
	}

	// Create new calculator with newer manifest time (after cache was created)
	// The cache was created "now" but the manifest is from "future", so cache should be invalid
	newManifestTime := time.Now().Add(30 * time.Minute) // Future time

	calculator2 := NewCalculator(1)
	err = calculator2.EnableMetadataCache(cacheDir, tempDir, "test", "app", newManifestTime)
	if err != nil {
		t.Fatalf("EnableMetadataCache() failed: %v", err)
	}

	// Cache should be invalidated for newer manifest (cache created before manifest)
	if calculator2.metadataCache.IsValidForManifest(newManifestTime) {
		t.Error("Cache should be invalid for newer manifest time")
	}
}
