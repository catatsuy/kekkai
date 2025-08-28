package manifest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntegrationSymlinkAttackScenarios(t *testing.T) {
	// Complete integration tests for various attack scenarios
	tempDir := t.TempDir()
	ctx := context.Background()

	// Scenario 1: Initial legitimate setup
	t.Run("complete_attack_scenario", func(t *testing.T) {
		// Step 1: Create legitimate files and symlinks
		legitimateFile := filepath.Join(tempDir, "config.json")
		sensitiveFile := filepath.Join(tempDir, "sensitive.txt")
		publicLink := filepath.Join(tempDir, "public_config")

		// Create legitimate configuration
		if err := os.WriteFile(legitimateFile, []byte(`{"public": "data"}`), 0644); err != nil {
			t.Fatal(err)
		}

		// Create sensitive file
		if err := os.WriteFile(sensitiveFile, []byte("SECRET_KEY=abc123"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create legitimate symlink to public config
		if err := os.Symlink("config.json", publicLink); err != nil {
			t.Fatal(err)
		}

		// Generate initial manifest
		generator := NewGenerator(2)
		manifest, err := generator.Generate(ctx, tempDir, []string{"sensitive.txt"})
		if err != nil {
			t.Fatal(err)
		}

		// Initial verification should pass
		if err := manifest.Verify(ctx, tempDir, 2); err != nil {
			t.Errorf("Initial verification failed: %v", err)
		}

		// Step 2: Attacker tries to replace symlink with file containing "symlink:" prefix
		if err := os.Remove(publicLink); err != nil {
			t.Fatal(err)
		}

		// Attacker creates a file with symlink-like content trying to maintain same hash
		attackContent := "symlink:config.json"
		if err := os.WriteFile(publicLink, []byte(attackContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Verification should detect type change
		err = manifest.Verify(ctx, tempDir, 2)
		if err == nil {
			t.Error("Should detect symlink replaced with regular file")
		} else if !strings.Contains(err.Error(), "modified:") && !strings.Contains(err.Error(), "type") {
			t.Errorf("Expected type change error, got: %v", err)
		}

		// Step 3: Attacker tries to change symlink target to sensitive file
		if err := os.Remove(publicLink); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("sensitive.txt", publicLink); err != nil {
			t.Fatal(err)
		}

		// Verification should detect hash change (different target)
		err = manifest.Verify(ctx, tempDir, 2)
		if err == nil {
			t.Error("Should detect symlink target change")
		} else if !strings.Contains(err.Error(), "modified") {
			t.Errorf("Expected modification error, got: %v", err)
		}
	})

	// Scenario 2: Race condition attack attempt
	t.Run("race_condition_attack", func(t *testing.T) {
		raceDir := filepath.Join(tempDir, "race")
		if err := os.Mkdir(raceDir, 0755); err != nil {
			t.Fatal(err)
		}

		normalFile := filepath.Join(raceDir, "normal.txt")
		if err := os.WriteFile(normalFile, []byte("normal content"), 0644); err != nil {
			t.Fatal(err)
		}

		generator := NewGenerator(2)
		manifest, err := generator.Generate(ctx, raceDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Simulate attack: quickly switch between file and symlink
		for i := 0; i < 5; i++ {
			// Replace with symlink
			if err := os.Remove(normalFile); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/etc/passwd", normalFile); err != nil {
				t.Fatal(err)
			}

			// Verification should catch the type change
			err := manifest.Verify(ctx, raceDir, 2)
			if err == nil {
				t.Error("Should detect file type change in race condition")
			}

			// Restore original
			if err := os.Remove(normalFile); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(normalFile, []byte("normal content"), 0644); err != nil {
				t.Fatal(err)
			}

			// Should pass with original
			err = manifest.Verify(ctx, raceDir, 2)
			if err != nil {
				t.Errorf("Should pass with original file: %v", err)
			}
		}
	})

	// Scenario 3: Complex symlink chain manipulation
	t.Run("symlink_chain_manipulation", func(t *testing.T) {
		chainDir := filepath.Join(tempDir, "chain")
		if err := os.Mkdir(chainDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create chain: link3 -> link2 -> link1 -> target
		target := filepath.Join(chainDir, "target.txt")
		link1 := filepath.Join(chainDir, "link1")
		link2 := filepath.Join(chainDir, "link2")
		link3 := filepath.Join(chainDir, "link3")

		if err := os.WriteFile(target, []byte("target content"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("target.txt", link1); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("link1", link2); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("link2", link3); err != nil {
			t.Fatal(err)
		}

		generator := NewGenerator(2)
		manifest, err := generator.Generate(ctx, chainDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Try to manipulate middle of chain
		if err := os.Remove(link2); err != nil {
			t.Fatal(err)
		}
		// Replace with direct link to different target
		maliciousTarget := filepath.Join(chainDir, "malicious.txt")
		if err := os.WriteFile(maliciousTarget, []byte("malicious"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("malicious.txt", link2); err != nil {
			t.Fatal(err)
		}

		// Should detect the change
		err = manifest.Verify(ctx, chainDir, 2)
		if err == nil {
			t.Error("Should detect symlink chain manipulation")
		}
	})

	// Scenario 4: Hidden file attack via symlink
	t.Run("hidden_file_via_symlink", func(t *testing.T) {
		hiddenDir := filepath.Join(tempDir, "hidden")
		if err := os.Mkdir(hiddenDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create visible file
		visibleFile := filepath.Join(hiddenDir, "visible.txt")
		if err := os.WriteFile(visibleFile, []byte("visible"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create symlink
		link := filepath.Join(hiddenDir, "link")
		if err := os.Symlink("visible.txt", link); err != nil {
			t.Fatal(err)
		}

		generator := NewGenerator(2)
		manifest, err := generator.Generate(ctx, hiddenDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Create hidden file
		hiddenFile := filepath.Join(hiddenDir, ".hidden")
		if err := os.WriteFile(hiddenFile, []byte("hidden content"), 0644); err != nil {
			t.Fatal(err)
		}

		// Try to point symlink to hidden file
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(".hidden", link); err != nil {
			t.Fatal(err)
		}

		// Should detect the change
		err = manifest.Verify(ctx, hiddenDir, 2)
		if err == nil {
			t.Error("Should detect symlink retargeting to hidden file")
		} else if !strings.Contains(err.Error(), "modified") && !strings.Contains(err.Error(), "added") {
			t.Errorf("Unexpected error: %v", err)
		}
	})

	// Scenario 5: Size-based attack detection
	t.Run("size_based_attack_detection", func(t *testing.T) {
		sizeDir := filepath.Join(tempDir, "size")
		if err := os.Mkdir(sizeDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create files with specific sizes
		smallFile := filepath.Join(sizeDir, "small.txt")
		largeFile := filepath.Join(sizeDir, "large.txt")

		if err := os.WriteFile(smallFile, []byte("small"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create large content
		largeContent := make([]byte, 10000)
		for i := range largeContent {
			largeContent[i] = 'A'
		}
		if err := os.WriteFile(largeFile, largeContent, 0644); err != nil {
			t.Fatal(err)
		}

		generator := NewGenerator(2)
		manifest, err := generator.Generate(ctx, sizeDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Try to swap files (simulating hash collision but different sizes)
		// This tests that size verification provides additional security
		if err := os.WriteFile(smallFile, largeContent, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(largeFile, []byte("small"), 0644); err != nil {
			t.Fatal(err)
		}

		err = manifest.Verify(ctx, sizeDir, 2)
		if err == nil {
			t.Error("Should detect file size changes")
		}
		// The error will be about hash mismatch since content changed
		// But size is also verified as part of FileInfo comparison
	})

	// Scenario 6: Symlink with same name as excluded file
	t.Run("symlink_excluded_name_attack", func(t *testing.T) {
		excludeDir := filepath.Join(tempDir, "exclude")
		if err := os.Mkdir(excludeDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create files
		configFile := filepath.Join(excludeDir, "config.txt")
		logFile := filepath.Join(excludeDir, "app.log")

		if err := os.WriteFile(configFile, []byte("config"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(logFile, []byte("log data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Generate manifest excluding .log files
		generator := NewGenerator(2)
		manifest, err := generator.Generate(ctx, excludeDir, []string{"*.log"})
		if err != nil {
			t.Fatal(err)
		}

		// Remove log file and create symlink with same name
		if err := os.Remove(logFile); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/etc/passwd", logFile); err != nil {
			t.Fatal(err)
		}

		// Verification should still pass (file is excluded)
		err = manifest.Verify(ctx, excludeDir, 2)
		if err != nil {
			t.Errorf("Excluded files should not affect verification: %v", err)
		}

		// But if we create a symlink with included name
		includedLink := filepath.Join(excludeDir, "newlink.txt")
		if err := os.Symlink("/etc/passwd", includedLink); err != nil {
			t.Fatal(err)
		}

		// Should detect the added file
		err = manifest.Verify(ctx, excludeDir, 2)
		if err == nil {
			t.Error("Should detect added symlink")
		} else if !strings.Contains(err.Error(), "added") {
			t.Errorf("Expected 'added' error, got: %v", err)
		}
	})
}

func TestManifestGenerationAndVerificationPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	tempDir := t.TempDir()
	ctx := context.Background()

	// Create many files and symlinks
	numFiles := 100
	for i := 0; i < numFiles; i++ {
		file := filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(file, []byte(fmt.Sprintf("content%d", i)), 0644); err != nil {
			t.Fatal(err)
		}

		if i%2 == 0 {
			link := filepath.Join(tempDir, fmt.Sprintf("link%d", i))
			if err := os.Symlink(fmt.Sprintf("file%d.txt", i), link); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Test generation performance
	start := time.Now()
	generator := NewGeneratorWithRateLimit(4, 10*1024*1024) // 10MB/s
	manifest, err := generator.Generate(ctx, tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	genDuration := time.Since(start)

	t.Logf("Generation took %v for %d files", genDuration, manifest.FileCount)

	// Test verification performance
	start = time.Now()
	err = manifest.VerifyWithRateLimit(ctx, tempDir, 4, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	verifyDuration := time.Since(start)

	t.Logf("Verification took %v for %d files", verifyDuration, manifest.FileCount)

	// Test cache-based verification performance
	cacheDir := t.TempDir()
	start = time.Now()
	err = manifest.VerifyWithCache(ctx, tempDir, cacheDir, "test", "app", 4, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	cacheVerifyDuration := time.Since(start)

	t.Logf("Cache verification took %v for %d files", cacheVerifyDuration, manifest.FileCount)

	// Second cache verification should be faster
	start = time.Now()
	err = manifest.VerifyWithCache(ctx, tempDir, cacheDir, "test", "app", 4, 0.0)
	if err != nil {
		t.Fatal(err)
	}
	cache2Duration := time.Since(start)

	t.Logf("Second cache verification took %v (should be faster)", cache2Duration)

	if cache2Duration >= cacheVerifyDuration {
		t.Log("Warning: Second cache verification was not faster")
	}
}

func TestConcurrentVerification(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// Create test files
	for i := 0; i < 10; i++ {
		file := filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(file, []byte(fmt.Sprintf("content%d", i)), 0644); err != nil {
			t.Fatal(err)
		}
	}

	generator := NewGenerator(2)
	manifest, err := generator.Generate(ctx, tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Run multiple verifications concurrently
	done := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			done <- manifest.Verify(ctx, tempDir, 2)
		}()
	}

	// All should succeed
	for i := 0; i < 5; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent verification %d failed: %v", i, err)
		}
	}
}

func TestManifestBackwardCompatibility(t *testing.T) {
	// Test that manifests with and without symlink fields work correctly
	tempDir := t.TempDir()
	ctx := context.Background()

	file := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(file, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Generate current manifest
	generator := NewGenerator(1)
	manifest, err := generator.Generate(ctx, tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Verify all expected fields are present
	for _, f := range manifest.Files {
		if f.Path == "" {
			t.Error("Path should not be empty")
		}
		if f.Hash == "" {
			t.Error("Hash should not be empty")
		}
		// Size can be 0 for empty files
		// IsSymlink defaults to false
		// LinkTarget can be empty for non-symlinks
	}

	// Test verification still works
	err = manifest.Verify(ctx, tempDir, 1)
	if err != nil {
		t.Errorf("Verification failed: %v", err)
	}
}
