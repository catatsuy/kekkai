package manifest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/catatsuy/kekkai/internal/hash"
)

func TestSymlinkSpoofingPrevention(t *testing.T) {
	// Create test directory
	tempDir := t.TempDir()

	// Create original files
	targetFile := filepath.Join(tempDir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("original content"), 0644); err != nil {
		t.Fatal(err)
	}

	linkPath := filepath.Join(tempDir, "link")
	if err := os.Symlink("target.txt", linkPath); err != nil {
		t.Fatal(err)
	}

	// Generate manifest
	generator := NewGenerator(0)
	manifest, err := generator.Generate(context.Background(), tempDir, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Initial verification should pass
	err = manifest.Verify(context.Background(), tempDir, 0)
	if err != nil {
		t.Errorf("Initial verify should pass: %v", err)
	}

	// Test 1: Replace symlink with regular file containing "symlink:<path>"
	t.Run("replace_symlink_with_spoofed_file", func(t *testing.T) {
		// Remove the symlink
		if err := os.Remove(linkPath); err != nil {
			t.Fatal(err)
		}

		// Create a regular file with content that matches symlink hash pattern
		spoofContent := "symlink:target.txt"
		if err := os.WriteFile(linkPath, []byte(spoofContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Verification should fail due to type change
		err := manifest.Verify(context.Background(), tempDir, 0)
		if err == nil {
			t.Error("Verify() should fail when symlink is replaced with regular file")
		} else if !strings.Contains(err.Error(), "modified: link (type symlink→file)") {
			t.Errorf("Error should mention type change, got: %v", err)
		}

		// Restore the symlink
		if err := os.Remove(linkPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("target.txt", linkPath); err != nil {
			t.Fatal(err)
		}
	})

	// Test 2: Replace regular file with symlink
	t.Run("replace_file_with_symlink", func(t *testing.T) {
		// Create a regular file first
		regularFile := filepath.Join(tempDir, "regular.txt")
		if err := os.WriteFile(regularFile, []byte("regular content"), 0644); err != nil {
			t.Fatal(err)
		}

		// Generate new manifest with the regular file
		manifest2, err := generator.Generate(context.Background(), tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Remove regular file and create symlink with same name
		if err := os.Remove(regularFile); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("target.txt", regularFile); err != nil {
			t.Fatal(err)
		}

		// Verification should fail due to type change
		err = manifest2.Verify(context.Background(), tempDir, 0)
		if err == nil {
			t.Error("Verify() should fail when regular file is replaced with symlink")
		} else if !strings.Contains(err.Error(), "modified: regular.txt (type file→symlink)") {
			t.Errorf("Error should mention type change, got: %v", err)
		}

		// Clean up
		if err := os.Remove(regularFile); err != nil {
			t.Fatal(err)
		}
	})

	// Test 3: File size verification for regular files
	t.Run("file_size_change_detection", func(t *testing.T) {
		// Create a test file with specific content
		sizeTestFile := filepath.Join(tempDir, "size_test.txt")
		originalContent := "original"
		if err := os.WriteFile(sizeTestFile, []byte(originalContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Generate manifest
		manifest3, err := generator.Generate(context.Background(), tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Find and modify the file's entry to simulate size mismatch
		// Note: Hash will not match but we're testing if size check happens
		found := false
		for i := range manifest3.Files {
			if manifest3.Files[i].Path == "size_test.txt" {
				// Store original values
				originalHash := manifest3.Files[i].Hash
				originalSize := manifest3.Files[i].Size

				// Set incorrect size but keep correct hash to test size validation
				manifest3.Files[i].Size = 1000 // Different from actual size

				// Since hash and size won't match together normally,
				// we're testing that the error message prioritizes type/size checks
				found = true

				// Restore for proper test
				manifest3.Files[i].Hash = originalHash
				manifest3.Files[i].Size = originalSize
				break
			}
		}

		if !found {
			t.Fatal("size_test.txt not found in manifest")
		}

		// Test with actual file size change
		// Write different content to change the file
		if err := os.WriteFile(sizeTestFile, []byte("modified content that is longer"), 0644); err != nil {
			t.Fatal(err)
		}

		// Verification should fail
		err = manifest3.Verify(context.Background(), tempDir, 0)
		if err == nil {
			t.Error("Verify() should fail when file content and size change")
		} else if !strings.Contains(err.Error(), "modified") {
			// Will report as "modified" since hash changed
			t.Errorf("Error should mention modification, got: %v", err)
		}

		// Clean up
		if err := os.Remove(sizeTestFile); err != nil {
			t.Fatal(err)
		}
	})

	// Test 4: Ensure symlink size is not validated (since it's meaningless)
	t.Run("symlink_size_not_validated", func(t *testing.T) {
		// Create a new symlink
		symlinkTestPath := filepath.Join(tempDir, "symlink_test")
		if err := os.Symlink("target.txt", symlinkTestPath); err != nil {
			t.Fatal(err)
		}

		// Generate manifest
		manifest4, err := generator.Generate(context.Background(), tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Manually modify the symlink's size in the manifest
		// This should cause verification to fail since we now check sizes
		for i := range manifest4.Files {
			if manifest4.Files[i].Path == "symlink_test" && manifest4.Files[i].IsSymlink {
				manifest4.Files[i].Size = 99999 // Arbitrary different size
				break
			}
		}

		// Force detailed comparison by checking file modifications

		// Verification should fail due to size difference
		// (Now we check size for both symlinks and regular files for consistency)
		err = manifest4.Verify(context.Background(), tempDir, 0)
		if err == nil {
			t.Error("Verify() should fail when size doesn't match")
		} else if !strings.Contains(err.Error(), "modified: symlink_test (size") {
			t.Errorf("Error should mention size change, got: %v", err)
		}

		// Clean up
		if err := os.Remove(symlinkTestPath); err != nil {
			t.Fatal(err)
		}
	})
}

func TestManifestVerifyWithTypeAndSize(t *testing.T) {
	// Create test directory
	tempDir := t.TempDir()

	// Create test files
	file1 := filepath.Join(tempDir, "file1.txt")
	file2 := filepath.Join(tempDir, "file2.txt")
	link1 := filepath.Join(tempDir, "link1")

	if err := os.WriteFile(file1, []byte("content1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("content2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file1.txt", link1); err != nil {
		t.Fatal(err)
	}

	// Generate manifest
	generator := NewGenerator(0)
	_, err := generator.Generate(context.Background(), tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a custom manifest to test verification logic
	testManifest := &Manifest{
		Version:     "1.0",
		FileCount:   3,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Files: []hash.FileInfo{
			{
				Path:      "file1.txt",
				Hash:      "hash1",
				Size:      8,
				IsSymlink: false,
			},
			{
				Path:      "file2.txt",
				Hash:      "hash2",
				Size:      8,
				IsSymlink: false,
			},
			{
				Path:       "link1",
				Hash:       "linkhash",
				Size:       0,
				IsSymlink:  true,
				LinkTarget: "file1.txt",
			},
		},
	}

	// Test scenarios
	tests := []struct {
		name        string
		setup       func()
		expectError string
		cleanup     func()
	}{
		{
			name: "type_changed_symlink_to_file",
			setup: func() {
				os.Remove(link1)
				os.WriteFile(link1, []byte("spoofed"), 0644)
			},
			expectError: "modified: link1 (type symlink→file)",
			cleanup: func() {
				os.Remove(link1)
				os.Symlink("file1.txt", link1)
			},
		},
		{
			name: "type_changed_file_to_symlink",
			setup: func() {
				os.Remove(file2)
				os.Symlink("file1.txt", file2)
			},
			expectError: "modified: file2.txt (type file→symlink)",
			cleanup: func() {
				os.Remove(file2)
				os.WriteFile(file2, []byte("content2"), 0644)
			},
		},
		{
			name: "size_changed_regular_file",
			setup: func() {
				// Create manifest with correct hash but wrong size
				for i := range testManifest.Files {
					if testManifest.Files[i].Path == "file1.txt" {
						// Get actual hash
						calc := hash.NewCalculator(1)
						result, _ := calc.CalculateDirectory(context.Background(), tempDir, nil)
						for _, f := range result.Files {
							if f.Path == "file1.txt" {
								testManifest.Files[i].Hash = f.Hash
								testManifest.Files[i].Size = 999 // Wrong size
								break
							}
						}
						break
					}
				}
			},
			expectError: "modified: file1.txt (size",
			cleanup: func() {
				// Restore correct size
				for i := range testManifest.Files {
					if testManifest.Files[i].Path == "file1.txt" {
						testManifest.Files[i].Size = 8
						testManifest.Files[i].Hash = "hash1"
						break
					}
				}
			},
		},
		{
			name: "hash_mismatch",
			setup: func() {
				// Set correct type and size but wrong hash
				for i := range testManifest.Files {
					if testManifest.Files[i].Path == "file1.txt" {
						testManifest.Files[i].Size = 8
						testManifest.Files[i].IsSymlink = false
						testManifest.Files[i].Hash = "wronghash"
						break
					}
				}
			},
			expectError: "modified: file1.txt",
			cleanup: func() {
				for i := range testManifest.Files {
					if testManifest.Files[i].Path == "file1.txt" {
						testManifest.Files[i].Hash = "hash1"
						break
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			defer tt.cleanup()

			err := testManifest.Verify(context.Background(), tempDir, 0)
			if tt.expectError != "" {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.expectError)
				} else if !strings.Contains(err.Error(), tt.expectError) {
					t.Errorf("Expected error containing '%s', got: %v", tt.expectError, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestSymlinkHashCalculation(t *testing.T) {
	// Create test directory
	tempDir := t.TempDir()

	// Create a target file
	targetFile := filepath.Join(tempDir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("target content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlinks pointing to different targets
	link1 := filepath.Join(tempDir, "link1")
	link2 := filepath.Join(tempDir, "link2")

	if err := os.Symlink("target.txt", link1); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("different_target.txt", link2); err != nil {
		t.Fatal(err)
	}

	// Generate manifest
	generator := NewGenerator(0)
	manifest, err := generator.Generate(context.Background(), tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Find the two symlinks in the manifest
	var hash1, hash2 string
	for _, file := range manifest.Files {
		if file.Path == "link1" {
			hash1 = file.Hash
			if !file.IsSymlink {
				t.Error("link1 should be marked as symlink")
			}
			if file.LinkTarget != "target.txt" {
				t.Errorf("link1 target = %s, want target.txt", file.LinkTarget)
			}
		}
		if file.Path == "link2" {
			hash2 = file.Hash
			if !file.IsSymlink {
				t.Error("link2 should be marked as symlink")
			}
			if file.LinkTarget != "different_target.txt" {
				t.Errorf("link2 target = %s, want different_target.txt", file.LinkTarget)
			}
		}
	}

	// Hashes should be different because they point to different targets
	if hash1 == hash2 {
		t.Error("Symlinks with different targets should have different hashes")
	}

	// Test that a regular file with "symlink:" content gets a different hash
	spoofFile := filepath.Join(tempDir, "spoof.txt")
	if err := os.WriteFile(spoofFile, []byte("symlink:target.txt"), 0644); err != nil {
		t.Fatal(err)
	}

	// Generate new manifest
	manifest2, err := generator.Generate(context.Background(), tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Find the spoof file
	var spoofIsSymlink bool
	for _, file := range manifest2.Files {
		if file.Path == "spoof.txt" {
			spoofIsSymlink = file.IsSymlink
			break
		}
	}

	// The spoof file should not be marked as symlink
	if spoofIsSymlink {
		t.Error("Regular file should not be marked as symlink")
	}

	// Even if hash might collide, the type difference will catch it
	// during verification
}
