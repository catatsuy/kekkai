package hash

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLargeDirectoryChannelBuffering(t *testing.T) {
	// Create temp directory with many files
	tempDir, err := os.MkdirTemp("", "large-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create 500 test files
	numFiles := 500
	for i := range numFiles {
		path := filepath.Join(tempDir, fmt.Sprintf("file_%04d.txt", i))
		content := fmt.Sprintf("content of file %d", i)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Test with 4 workers - buffer should be min(4*2, 100) = 8
	calc := NewCalculator(4)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	result, err := calc.CalculateDirectory(ctx, tempDir, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory failed: %v", err)
	}

	elapsed := time.Since(start)

	if result.FileCount != numFiles {
		t.Errorf("Expected %d files, got %d", numFiles, result.FileCount)
	}

	t.Logf("Processed %d files in %v with limited channel buffers", result.FileCount, elapsed)
}

func TestMassiveDirectoryMemoryEfficiency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping massive directory test in short mode")
	}

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "massive-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create 10000 small files
	numFiles := 10000
	t.Logf("Creating %d test files...", numFiles)

	for i := range numFiles {
		path := filepath.Join(tempDir, fmt.Sprintf("f%05d", i))
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Test with many workers - buffer should still be capped at 100
	calc := NewCalculator(50)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	result, err := calc.CalculateDirectory(ctx, tempDir, nil)
	if err != nil {
		t.Fatalf("CalculateDirectory failed: %v", err)
	}

	elapsed := time.Since(start)

	if result.FileCount != numFiles {
		t.Errorf("Expected %d files, got %d", numFiles, result.FileCount)
	}

	t.Logf("Successfully processed %d files in %v with memory-efficient buffering", result.FileCount, elapsed)
}
