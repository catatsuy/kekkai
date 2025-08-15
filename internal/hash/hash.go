package hash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// FileInfo represents information about a single file or symlink
type FileInfo struct {
	Path       string `json:"path"`
	Hash       string `json:"hash"`
	Size       int64  `json:"size"`
	IsSymlink  bool   `json:"is_symlink,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
}

// Result represents the result of hash calculation
type Result struct {
	TotalHash string     `json:"total_hash"`
	Files     []FileInfo `json:"files"`
	FileCount int        `json:"file_count"`
}

// Calculator handles hash calculation for files and directories
type Calculator struct {
	numWorkers  int
	bufferSize  int
	bytesPerSec int64         // Rate limit in bytes per second (0 = no limit)
	limiter     *rate.Limiter // Shared rate limiter for all workers
}

// throttledCopy performs io.CopyBuffer with rate limiting
func throttledCopy(ctx context.Context, dst io.Writer, src io.Reader, buf []byte, limiter *rate.Limiter, bytesPerSec int64) (int64, error) {
	maxChunk := len(buf)
	if int64(maxChunk) > bytesPerSec {
		maxChunk = int(bytesPerSec)
	}
	if maxChunk > 64*1024 {
		maxChunk = 64 * 1024
	}

	var written int64
	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		// Read with limited chunk size
		nr := maxChunk
		if nr > len(buf) {
			nr = len(buf)
		}

		// Wait for rate limit
		if err := limiter.WaitN(ctx, nr); err != nil {
			return written, err
		}

		// Read from source
		readBytes, readErr := src.Read(buf[:nr])
		if readBytes > 0 {
			// Write to destination
			writtenBytes, writeErr := dst.Write(buf[0:readBytes])
			if writtenBytes > 0 {
				written += int64(writtenBytes)
			}
			if writeErr != nil {
				return written, writeErr
			}
			if readBytes != writtenBytes {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
}

// NewCalculator creates a calculator with custom worker count
func NewCalculator(numWorkers int) *Calculator {
	if numWorkers <= 0 {
		numWorkers = runtime.GOMAXPROCS(0)
	}

	return &Calculator{
		numWorkers:  numWorkers,
		bufferSize:  1024 * 1024, // 1MB buffer
		bytesPerSec: 0,           // No rate limit by default
	}
}

// NewCalculatorWithRateLimit creates a calculator with rate limiting
func NewCalculatorWithRateLimit(numWorkers int, bytesPerSec int64) *Calculator {
	if numWorkers <= 0 {
		numWorkers = runtime.GOMAXPROCS(0)
	}

	var limiter *rate.Limiter
	if bytesPerSec > 0 {
		// Create rate limiter with burst equal to buffer size or 1MB, whichever is smaller
		burstSize := int(bytesPerSec)
		if burstSize > 1024*1024 {
			burstSize = 1024 * 1024 // Max 1MB burst
		}
		limiter = rate.NewLimiter(rate.Limit(bytesPerSec), burstSize)
	}

	return &Calculator{
		numWorkers:  numWorkers,
		bufferSize:  1024 * 1024, // 1MB buffer
		bytesPerSec: bytesPerSec,
		limiter:     limiter,
	}
}

// CalculateDirectory calculates hash for all files in a directory with context
func (c *Calculator) CalculateDirectory(ctx context.Context, rootDir string, excludes []string) (*Result, error) {
	// Resolve symlink if the target directory itself is a symlink
	resolvedDir, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve target directory: %w", err)
	}

	// Collect files
	files, err := c.collectFiles(resolvedDir, excludes)
	if err != nil {
		return nil, fmt.Errorf("failed to collect files: %w", err)
	}

	// Calculate hashes in parallel
	fileInfos, err := c.calculateFileHashes(ctx, resolvedDir, files)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate file hashes: %w", err)
	}

	// Sort for deterministic order
	sort.Slice(fileInfos, func(i, j int) bool {
		return fileInfos[i].Path < fileInfos[j].Path
	})

	// Calculate total hash
	totalHash := c.calculateTotalHash(fileInfos)

	return &Result{
		TotalHash: totalHash,
		Files:     fileInfos,
		FileCount: len(fileInfos),
	}, nil
}

// collectFiles walks the directory and collects files based on patterns
func (c *Calculator) collectFiles(rootDir string, excludes []string) ([]string, error) {
	files := make([]string, 0, 50) // Start with capacity for 50 files

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}

		// Normalize path (use forward slash even on Windows)
		relPath = filepath.ToSlash(relPath)

		// Check exclude patterns
		if matchExcludePatterns(relPath, excludes) {
			return nil
		}

		files = append(files, path)
		return nil
	})

	return files, err
}

// calculateFileHashes calculates hashes for multiple files in parallel
func (c *Calculator) calculateFileHashes(ctx context.Context, rootDir string, files []string) ([]FileInfo, error) {
	var wg sync.WaitGroup
	// Use smaller buffer sizes to avoid excessive memory usage with large directories
	// Buffer size is min(numWorkers * 2, 100) to balance between performance and memory
	bufferSize := min(c.numWorkers*2, 100)
	jobs := make(chan string, bufferSize)
	results := make(chan FileInfo, bufferSize)
	errors := make(chan error, bufferSize)

	// Start workers
	for i := 0; i < c.numWorkers; i++ {
		wg.Go(func() {
			// Create reusable hasher and buffer for this worker
			hasher := sha256.New()
			buf := make([]byte, c.bufferSize)

			for {
				select {
				case <-ctx.Done():
					return
				case path, ok := <-jobs:
					if !ok {
						return
					}

					info, err := os.Lstat(path) // Use Lstat to get symlink info
					if err != nil {
						errors <- fmt.Errorf("failed to stat %s: %w", path, err)
						continue
					}

					relPath, _ := filepath.Rel(rootDir, path)
					relPath = filepath.ToSlash(relPath)

					// Handle symlinks
					if info.Mode()&os.ModeSymlink != 0 {
						target, err := os.Readlink(path)
						if err != nil {
							errors <- fmt.Errorf("failed to read symlink %s: %w", path, err)
							continue
						}

						// Create a hash based on the symlink target path
						// This ensures changes to symlink targets are detected
						hasher.Reset()
						hasher.Write([]byte("symlink:" + target))
						hash := hex.EncodeToString(hasher.Sum(nil))

						results <- FileInfo{
							Path:       relPath,
							Hash:       hash,
							Size:       0,
							IsSymlink:  true,
							LinkTarget: target,
						}
					} else {
						// Regular file
						hash, err := c.hashFileWithHasher(ctx, path, hasher, buf)
						if err != nil {
							errors <- fmt.Errorf("failed to hash %s: %w", path, err)
							continue
						}

						results <- FileInfo{
							Path: relPath,
							Hash: hash,
							Size: info.Size(),
						}
					}
				}
			}
		})
	}

	// Send jobs
	go func() {
		for _, file := range files {
			select {
			case <-ctx.Done():
				close(jobs)
				return
			case jobs <- file:
			}
		}
		close(jobs)
	}()

	// Wait for completion
	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	// Collect results and errors
	fileInfos := make([]FileInfo, 0, len(files)) // Pre-allocate based on file count
	var collectedErrors []error

	// Collect all results
	for result := range results {
		fileInfos = append(fileInfos, result)
	}

	// Collect all errors
	for err := range errors {
		if err != nil {
			collectedErrors = append(collectedErrors, err)
		}
	}

	// Return first error if any
	if len(collectedErrors) > 0 {
		return nil, collectedErrors[0]
	}

	return fileInfos, nil
}

// hashFileWithHasher calculates hash of a file using provided hasher and buffer (for reuse)
func (c *Calculator) hashFileWithHasher(ctx context.Context, path string, hasher hash.Hash, buf []byte) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher.Reset()

	if c.bytesPerSec > 0 && c.limiter != nil {
		// Use throttled copy for rate limiting
		_, err = throttledCopy(ctx, hasher, file, buf, c.limiter, c.bytesPerSec)
	} else {
		// Normal copy
		_, err = io.CopyBuffer(hasher, file, buf)
	}

	if err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// calculateTotalHash calculates the combined hash of all files
func (c *Calculator) calculateTotalHash(files []FileInfo) string {
	hasher := sha256.New()

	for _, f := range files {
		// Format: "path:hash:size\n"
		// This ensures same files always produce same total hash
		line := fmt.Sprintf("%s:%s:%d\n", f.Path, f.Hash, f.Size)
		hasher.Write([]byte(line))
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// matchExcludePatterns checks if a path matches exclude patterns
func matchExcludePatterns(path string, excludes []string) bool {
	for _, pattern := range excludes {
		if matched := matchGlob(pattern, path); matched {
			return true
		}
	}
	return false
}

// matchGlob matches a glob pattern against a path
func matchGlob(pattern, path string) bool {
	// Handle ** for recursive matching
	if strings.Contains(pattern, "**") {
		// Special case: **/* matches everything
		if pattern == "**/*" || pattern == "**" {
			return true
		}

		// Handle patterns like "src/**" which should match everything under src/
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			return strings.HasPrefix(path, prefix+"/")
		}

		// Handle patterns like "**/*.js" which should match any .js file at any depth
		if strings.HasPrefix(pattern, "**/") {
			suffix := strings.TrimPrefix(pattern, "**/")
			matched, _ := filepath.Match(suffix, filepath.Base(path))
			return matched
		}

		// General case: replace ** with * for simple matching
		simplifiedPattern := strings.ReplaceAll(pattern, "**", "*")
		matched, _ := filepath.Match(simplifiedPattern, path)
		return matched
	}

	// Simple glob matching
	matched, _ := filepath.Match(pattern, path)
	return matched
}

// VerifyIntegrity verifies the integrity of files against a manifest
func VerifyIntegrity(ctx context.Context, manifest *Result, targetDir string) error {
	calculator := NewCalculator(0)

	// Resolve symlink if the target directory itself is a symlink
	resolvedDir, err := filepath.EvalSymlinks(targetDir)
	if err != nil {
		return fmt.Errorf("failed to resolve target directory: %w", err)
	}

	// Calculate current state
	current, err := calculator.CalculateDirectory(ctx, resolvedDir, nil)
	if err != nil {
		return fmt.Errorf("failed to calculate current hash: %w", err)
	}

	// Quick check with total hash
	if manifest.TotalHash == current.TotalHash {
		return nil // All files are intact
	}

	// Detailed comparison
	manifestMap := make(map[string]string)
	for _, f := range manifest.Files {
		manifestMap[f.Path] = f.Hash
	}

	currentMap := make(map[string]string)
	for _, f := range current.Files {
		currentMap[f.Path] = f.Hash
	}

	issues := make([]string, 0, 10) // Start with capacity for 10 issues

	// Check for modified or deleted files
	for path, expectedHash := range manifestMap {
		if actualHash, exists := currentMap[path]; exists {
			if expectedHash != actualHash {
				issues = append(issues, fmt.Sprintf("modified: %s", path))
			}
		} else {
			issues = append(issues, fmt.Sprintf("deleted: %s", path))
		}
	}

	// Check for added files
	for path := range currentMap {
		if _, exists := manifestMap[path]; !exists {
			issues = append(issues, fmt.Sprintf("added: %s", path))
		}
	}

	if len(issues) > 0 {
		return fmt.Errorf("integrity check failed:\n%s", strings.Join(issues, "\n"))
	}

	return nil
}

// VerifyIntegrityWithPatterns verifies the integrity of files against a manifest with patterns
func VerifyIntegrityWithPatterns(ctx context.Context, manifest *Result, targetDir string, excludes []string) error {
	calculator := NewCalculator(0)

	// Resolve symlink if the target directory itself is a symlink
	resolvedDir, err := filepath.EvalSymlinks(targetDir)
	if err != nil {
		return fmt.Errorf("failed to resolve target directory: %w", err)
	}

	// Calculate current state with same patterns
	current, err := calculator.CalculateDirectory(ctx, resolvedDir, excludes)
	if err != nil {
		return fmt.Errorf("failed to calculate current hash: %w", err)
	}

	// Quick check with total hash
	if manifest.TotalHash == current.TotalHash {
		return nil // All files are intact
	}

	// Detailed comparison
	manifestMap := make(map[string]string)
	for _, f := range manifest.Files {
		manifestMap[f.Path] = f.Hash
	}

	currentMap := make(map[string]string)
	for _, f := range current.Files {
		currentMap[f.Path] = f.Hash
	}

	issues := make([]string, 0, 10) // Start with capacity for 10 issues

	// Check for modified or deleted files
	for path, expectedHash := range manifestMap {
		if actualHash, exists := currentMap[path]; exists {
			if expectedHash != actualHash {
				issues = append(issues, fmt.Sprintf("modified: %s", path))
			}
		} else {
			issues = append(issues, fmt.Sprintf("deleted: %s", path))
		}
	}

	// Check for added files
	for path := range currentMap {
		if _, exists := manifestMap[path]; !exists {
			issues = append(issues, fmt.Sprintf("added: %s", path))
		}
	}

	if len(issues) > 0 {
		return fmt.Errorf("integrity check failed:\n%s", strings.Join(issues, "\n"))
	}

	return nil
}
