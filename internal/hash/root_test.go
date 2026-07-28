package hash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRootRejectsEscapingFileJobs(t *testing.T) {
	parentDir := t.TempDir()
	targetDir := filepath.Join(parentDir, "target")
	if err := os.Mkdir(targetDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "inside.txt"), []byte("inside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parentDir, "outside.txt"), []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	calc := NewCalculator(1)
	tests := []struct {
		name    string
		relPath string
	}{
		{
			name:    "parent traversal",
			relPath: "../outside.txt",
		},
		{
			name:    "absolute path",
			relPath: filepath.ToSlash(filepath.Join(targetDir, "inside.txt")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := []fileJob{{relPath: tt.relPath}}
			if _, err := calc.calculateFileHashes(context.Background(), root, targetDir, jobs); err == nil {
				t.Errorf("calculateFileHashes() accepted escaping path %q", tt.relPath)
			}

			hasher := sha256.New()
			buf := make([]byte, calc.bufferSize)
			if _, err := calc.hashFileWithHasher(context.Background(), root, tt.relPath, hasher, buf); err == nil {
				t.Errorf("hashFileWithHasher() accepted escaping path %q", tt.relPath)
			}
		})
	}
}

func TestRootSymlinksHashLinkTargetWithoutReadingContent(t *testing.T) {
	parentDir := t.TempDir()
	targetDir := filepath.Join(parentDir, "target")
	if err := os.Mkdir(targetDir, 0755); err != nil {
		t.Fatal(err)
	}

	insidePath := filepath.Join(targetDir, "inside.txt")
	outsidePath := filepath.Join(parentDir, "outside.txt")
	if err := os.WriteFile(insidePath, []byte("inside content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsidePath, []byte("outside content"), 0644); err != nil {
		t.Fatal(err)
	}

	links := map[string]string{
		"inside-link":  "inside.txt",
		"outside-link": "../outside.txt",
		"broken-link":  "missing.txt",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(targetDir, name)); err != nil {
			t.Fatal(err)
		}
	}

	calc := NewCalculator(2)
	result, err := calc.CalculateDirectory(context.Background(), targetDir, []string{"inside.txt"})
	if err != nil {
		t.Fatalf("CalculateDirectory() error = %v", err)
	}
	assertSymlinkHashes(t, result, links)

	if err := os.WriteFile(insidePath, []byte("changed inside content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsidePath, []byte("changed outside content"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err = calc.CalculateDirectory(context.Background(), targetDir, []string{"inside.txt"})
	if err != nil {
		t.Fatalf("CalculateDirectory() after target content changes error = %v", err)
	}
	assertSymlinkHashes(t, result, links)
}

func assertSymlinkHashes(t *testing.T, result *Result, links map[string]string) {
	t.Helper()

	if result.FileCount != len(links) {
		t.Fatalf("FileCount = %d, want %d", result.FileCount, len(links))
	}

	files := make(map[string]FileInfo, len(result.Files))
	for _, file := range result.Files {
		files[file.Path] = file
	}

	for name, target := range links {
		file, ok := files[name]
		if !ok {
			t.Errorf("symlink %q not found", name)
			continue
		}
		if !file.IsSymlink {
			t.Errorf("%q is not marked as a symlink", name)
		}
		if file.LinkTarget != target {
			t.Errorf("%q LinkTarget = %q, want %q", name, file.LinkTarget, target)
		}

		sum := sha256.Sum256([]byte("symlink:" + target))
		expectedHash := hex.EncodeToString(sum[:])
		if file.Hash != expectedHash {
			t.Errorf("%q Hash = %q, want %q", name, file.Hash, expectedHash)
		}
	}
}
