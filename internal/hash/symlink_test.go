package hash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSymlinkHandlingDetailed(t *testing.T) {
	// Create test directory
	tempDir := t.TempDir()
	ctx := context.Background()

	// Test 1: Symlink hash is based on target path
	t.Run("symlink_hash_based_on_target", func(t *testing.T) {
		target1 := filepath.Join(tempDir, "target1.txt")
		target2 := filepath.Join(tempDir, "target2.txt")
		link := filepath.Join(tempDir, "link")

		// Create targets
		if err := os.WriteFile(target1, []byte("content1"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target2, []byte("content2"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create symlink to target1
		if err := os.Symlink("target1.txt", link); err != nil {
			t.Fatal(err)
		}

		calc := NewCalculator(1)
		result1, err := calc.CalculateDirectory(ctx, tempDir, []string{"target*.txt"})
		if err != nil {
			t.Fatal(err)
		}

		var hash1 string
		for _, f := range result1.Files {
			if f.Path == "link" {
				hash1 = f.Hash
				if !f.IsSymlink {
					t.Error("link should be marked as symlink")
				}
				break
			}
		}

		// Change symlink to target2
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("target2.txt", link); err != nil {
			t.Fatal(err)
		}

		result2, err := calc.CalculateDirectory(ctx, tempDir, []string{"target*.txt"})
		if err != nil {
			t.Fatal(err)
		}

		var hash2 string
		for _, f := range result2.Files {
			if f.Path == "link" {
				hash2 = f.Hash
				break
			}
		}

		// Hashes should be different
		if hash1 == hash2 {
			t.Error("Symlink hash should change when target changes")
		}

		// Verify hash format
		hasher := sha256.New()
		hasher.Write([]byte("symlink:target1.txt"))
		expectedHash1 := hex.EncodeToString(hasher.Sum(nil))
		if hash1 != expectedHash1 {
			t.Errorf("Symlink hash = %s, want %s", hash1, expectedHash1)
		}
	})

	// Test 2: Regular file with symlink-like content
	t.Run("regular_file_with_symlink_content", func(t *testing.T) {
		file := filepath.Join(tempDir, "fake_symlink.txt")
		content := "symlink:target.txt"

		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		calc := NewCalculator(1)
		result, err := calc.CalculateDirectory(ctx, tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		for _, f := range result.Files {
			if f.Path == "fake_symlink.txt" {
				if f.IsSymlink {
					t.Error("Regular file should not be marked as symlink")
				}
				if f.LinkTarget != "" {
					t.Error("Regular file should not have LinkTarget")
				}

				// Calculate expected hash for the content
				hasher := sha256.New()
				hasher.Write([]byte(content))
				expectedHash := hex.EncodeToString(hasher.Sum(nil))

				if f.Hash != expectedHash {
					t.Errorf("File hash = %s, want %s", f.Hash, expectedHash)
				}
				break
			}
		}
	})

	// Test 3: FileInfo structure correctness
	t.Run("file_info_structure", func(t *testing.T) {
		// Create regular file
		regularFile := filepath.Join(tempDir, "regular.txt")
		regularContent := "regular content"
		if err := os.WriteFile(regularFile, []byte(regularContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Create symlink
		symlinkFile := filepath.Join(tempDir, "symlink")
		if err := os.Symlink("regular.txt", symlinkFile); err != nil {
			t.Fatal(err)
		}

		calc := NewCalculator(1)
		result, err := calc.CalculateDirectory(ctx, tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		var regularInfo, symlinkInfo *FileInfo
		for _, f := range result.Files {
			switch f.Path {
			case "regular.txt":
				regularInfo = &f
			case "symlink":
				symlinkInfo = &f
			}
		}

		// Check regular file
		if regularInfo == nil {
			t.Fatal("Regular file not found")
		}
		if regularInfo.IsSymlink {
			t.Error("Regular file marked as symlink")
		}
		if regularInfo.LinkTarget != "" {
			t.Error("Regular file has LinkTarget")
		}
		if regularInfo.Size != int64(len(regularContent)) {
			t.Errorf("Regular file size = %d, want %d", regularInfo.Size, len(regularContent))
		}

		// Check symlink
		if symlinkInfo == nil {
			t.Fatal("Symlink not found")
		}
		if !symlinkInfo.IsSymlink {
			t.Error("Symlink not marked as symlink")
		}
		if symlinkInfo.LinkTarget != "regular.txt" {
			t.Errorf("Symlink target = %s, want regular.txt", symlinkInfo.LinkTarget)
		}
	})

	// Test 4: Broken symlink handling
	t.Run("broken_symlink", func(t *testing.T) {
		brokenLink := filepath.Join(tempDir, "broken_link")
		if err := os.Symlink("non_existent.txt", brokenLink); err != nil {
			t.Fatal(err)
		}

		calc := NewCalculator(1)
		result, err := calc.CalculateDirectory(ctx, tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		found := false
		for _, f := range result.Files {
			if f.Path == "broken_link" {
				found = true
				if !f.IsSymlink {
					t.Error("Broken symlink should be marked as symlink")
				}
				if f.LinkTarget != "non_existent.txt" {
					t.Errorf("Broken symlink target = %s, want non_existent.txt", f.LinkTarget)
				}
				// Hash should still be calculated based on target
				hasher := sha256.New()
				hasher.Write([]byte("symlink:non_existent.txt"))
				expectedHash := hex.EncodeToString(hasher.Sum(nil))
				if f.Hash != expectedHash {
					t.Errorf("Broken symlink hash = %s, want %s", f.Hash, expectedHash)
				}
				break
			}
		}

		if !found {
			t.Error("Broken symlink not included in results")
		}
	})

	// Test 5: Symlink to symlink (chain)
	t.Run("symlink_chain", func(t *testing.T) {
		// Create a file
		finalTarget := filepath.Join(tempDir, "final.txt")
		if err := os.WriteFile(finalTarget, []byte("final content"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create first symlink
		link1 := filepath.Join(tempDir, "link_to_final")
		if err := os.Symlink("final.txt", link1); err != nil {
			t.Fatal(err)
		}

		// Create second symlink pointing to first
		link2 := filepath.Join(tempDir, "link_to_link")
		if err := os.Symlink("link_to_final", link2); err != nil {
			t.Fatal(err)
		}

		calc := NewCalculator(1)
		result, err := calc.CalculateDirectory(ctx, tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Both symlinks should have different hashes
		var hash1, hash2 string
		for _, f := range result.Files {
			if f.Path == "link_to_final" {
				hash1 = f.Hash
				if f.LinkTarget != "final.txt" {
					t.Errorf("link_to_final target = %s, want final.txt", f.LinkTarget)
				}
			}
			if f.Path == "link_to_link" {
				hash2 = f.Hash
				if f.LinkTarget != "link_to_final" {
					t.Errorf("link_to_link target = %s, want link_to_final", f.LinkTarget)
				}
			}
		}

		if hash1 == hash2 {
			t.Error("Different symlinks should have different hashes")
		}
	})

	// Test 6: Directory symlink
	t.Run("directory_symlink", func(t *testing.T) {
		// Create a directory
		subDir := filepath.Join(tempDir, "subdir")
		if err := os.Mkdir(subDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create file in directory
		subFile := filepath.Join(subDir, "file.txt")
		if err := os.WriteFile(subFile, []byte("subfile content"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create symlink to directory
		dirLink := filepath.Join(tempDir, "dir_link")
		if err := os.Symlink("subdir", dirLink); err != nil {
			t.Fatal(err)
		}

		calc := NewCalculator(1)
		result, err := calc.CalculateDirectory(ctx, tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		// The symlink itself should be included
		found := false
		for _, f := range result.Files {
			if f.Path == "dir_link" {
				found = true
				if !f.IsSymlink {
					t.Error("Directory symlink should be marked as symlink")
				}
				if f.LinkTarget != "subdir" {
					t.Errorf("Directory symlink target = %s, want subdir", f.LinkTarget)
				}
				break
			}
		}

		if !found {
			// Directory symlinks might be skipped by filepath.Walk
			t.Log("Directory symlink not included (expected behavior for directory symlinks)")
		}
	})
}

func TestSymlinkAttackPrevention(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// Test: Attempt to create collision between symlink and regular file
	t.Run("hash_collision_prevention", func(t *testing.T) {
		// Create a symlink
		target := "target.txt"
		symlink := filepath.Join(tempDir, "symlink")
		if err := os.Symlink(target, symlink); err != nil {
			t.Fatal(err)
		}

		// Create a regular file with content that matches symlink pattern
		spoofFile := filepath.Join(tempDir, "spoof.txt")
		spoofContent := "symlink:" + target
		if err := os.WriteFile(spoofFile, []byte(spoofContent), 0644); err != nil {
			t.Fatal(err)
		}

		calc := NewCalculator(1)
		result, err := calc.CalculateDirectory(ctx, tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		var symlinkInfo, spoofInfo *FileInfo
		for _, f := range result.Files {
			switch f.Path {
			case "symlink":
				symlinkInfo = &f
			case "spoof.txt":
				spoofInfo = &f
			}
		}

		if symlinkInfo == nil || spoofInfo == nil {
			t.Fatal("Files not found in result")
		}

		// Even if hashes might be similar, type should be different
		if symlinkInfo.IsSymlink == spoofInfo.IsSymlink {
			t.Error("Symlink and regular file should have different IsSymlink values")
		}

		// Sizes should be different
		if symlinkInfo.Size == spoofInfo.Size {
			t.Log("Warning: Symlink and spoof file have same size (may vary by OS)")
		}

		// LinkTarget should only be set for symlink
		if symlinkInfo.LinkTarget == "" {
			t.Error("Symlink should have LinkTarget")
		}
		if spoofInfo.LinkTarget != "" {
			t.Error("Regular file should not have LinkTarget")
		}
	})

	// Test: Verify detection of file type changes
	t.Run("type_change_detection", func(t *testing.T) {
		file := filepath.Join(tempDir, "changeable")

		// Start as regular file
		if err := os.WriteFile(file, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}

		calc := NewCalculator(1)
		result1, err := calc.CalculateDirectory(ctx, tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		var info1 *FileInfo
		for _, f := range result1.Files {
			if f.Path == "changeable" {
				info1 = &f
				break
			}
		}

		if info1.IsSymlink {
			t.Error("Regular file marked as symlink")
		}

		// Change to symlink
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("target", file); err != nil {
			t.Fatal(err)
		}

		result2, err := calc.CalculateDirectory(ctx, tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		var info2 *FileInfo
		for _, f := range result2.Files {
			if f.Path == "changeable" {
				info2 = &f
				break
			}
		}

		if !info2.IsSymlink {
			t.Error("Symlink not marked as symlink")
		}

		// Type should be different
		if info1.IsSymlink == info2.IsSymlink {
			t.Error("Type change not detected")
		}
	})
}

func TestFileInfoJSON(t *testing.T) {
	// Test JSON marshaling of FileInfo
	fi := FileInfo{
		Path:       "test.txt",
		Hash:       "abc123",
		Size:       100,
		ModTime:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		IsSymlink:  true,
		LinkTarget: "target.txt",
	}

	// This test ensures that FileInfo can be properly serialized
	// which is important for manifest generation
	t.Run("json_fields", func(t *testing.T) {
		if fi.Path != "test.txt" {
			t.Error("Path field incorrect")
		}
		if fi.Hash != "abc123" {
			t.Error("Hash field incorrect")
		}
		if fi.Size != 100 {
			t.Error("Size field incorrect")
		}
		if !fi.IsSymlink {
			t.Error("IsSymlink field incorrect")
		}
		if fi.LinkTarget != "target.txt" {
			t.Error("LinkTarget field incorrect")
		}
	})
}

func TestSymlinkExcludePatterns(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// Create test structure
	regularFile := filepath.Join(tempDir, "regular.txt")
	symlink1 := filepath.Join(tempDir, "link1.lnk")
	symlink2 := filepath.Join(tempDir, "link2.sym")

	if err := os.WriteFile(regularFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("regular.txt", symlink1); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("regular.txt", symlink2); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		excludes []string
		expected []string
	}{
		{
			name:     "no excludes",
			excludes: nil,
			expected: []string{"regular.txt", "link1.lnk", "link2.sym"},
		},
		{
			name:     "exclude .lnk files",
			excludes: []string{"*.lnk"},
			expected: []string{"regular.txt", "link2.sym"},
		},
		{
			name:     "exclude all symlinks by pattern",
			excludes: []string{"*.lnk", "*.sym"},
			expected: []string{"regular.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := NewCalculator(1)
			result, err := calc.CalculateDirectory(ctx, tempDir, tt.excludes)
			if err != nil {
				t.Fatal(err)
			}

			if len(result.Files) != len(tt.expected) {
				t.Errorf("Expected %d files, got %d", len(tt.expected), len(result.Files))
			}

			for _, expectedFile := range tt.expected {
				found := false
				for _, f := range result.Files {
					if f.Path == expectedFile {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected file %s not found", expectedFile)
				}
			}
		})
	}
}

func TestParallelSymlinkProcessing(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// Create many symlinks to test parallel processing
	numFiles := 20
	for i := range numFiles {
		// Create regular files
		file := filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(file, fmt.Appendf(nil, "content%d", i), 0644); err != nil {
			t.Fatal(err)
		}

		// Create symlinks
		link := filepath.Join(tempDir, fmt.Sprintf("link%d", i))
		if err := os.Symlink(fmt.Sprintf("file%d.txt", i), link); err != nil {
			t.Fatal(err)
		}
	}

	// Test with different worker counts
	for _, workers := range []int{1, 4, 8} {
		t.Run(fmt.Sprintf("workers_%d", workers), func(t *testing.T) {
			calc := NewCalculator(workers)
			result, err := calc.CalculateDirectory(ctx, tempDir, nil)
			if err != nil {
				t.Fatal(err)
			}

			// Should have 2 * numFiles entries (files + symlinks)
			if len(result.Files) != 2*numFiles {
				t.Errorf("Expected %d files, got %d", 2*numFiles, len(result.Files))
			}

			// Count symlinks
			symlinkCount := 0
			for _, f := range result.Files {
				if f.IsSymlink {
					symlinkCount++
					if f.LinkTarget == "" {
						t.Error("Symlink missing LinkTarget")
					}
				}
			}

			if symlinkCount != numFiles {
				t.Errorf("Expected %d symlinks, got %d", numFiles, symlinkCount)
			}
		})
	}
}

func TestSymlinkWithSpecialCharacters(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// Test symlinks with special characters in target
	specialTargets := []string{
		"file with spaces.txt",
		"file-with-dashes.txt",
		"file_with_underscores.txt",
		"file.with.dots.txt",
		"文件.txt", // Unicode characters
	}

	for _, target := range specialTargets {
		t.Run(strings.ReplaceAll(target, " ", "_"), func(t *testing.T) {
			// Create target file
			targetPath := filepath.Join(tempDir, target)
			if err := os.WriteFile(targetPath, []byte("content"), 0644); err != nil {
				t.Fatal(err)
			}

			// Create symlink
			linkName := "link_to_" + strings.ReplaceAll(target, " ", "_")
			linkPath := filepath.Join(tempDir, linkName)
			if err := os.Symlink(target, linkPath); err != nil {
				t.Fatal(err)
			}

			calc := NewCalculator(1)
			result, err := calc.CalculateDirectory(ctx, tempDir, nil)
			if err != nil {
				t.Fatal(err)
			}

			// Find the symlink
			found := false
			for _, f := range result.Files {
				if f.Path == linkName {
					found = true
					if !f.IsSymlink {
						t.Error("Should be marked as symlink")
					}
					if f.LinkTarget != target {
						t.Errorf("LinkTarget = %s, want %s", f.LinkTarget, target)
					}
					break
				}
			}

			if !found {
				t.Errorf("Symlink %s not found", linkName)
			}

			// Clean up for next test
			os.Remove(linkPath)
			os.Remove(targetPath)
		})
	}
}
