package hash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/catatsuy/kekkai/internal/cache"
	"golang.org/x/time/rate"
)

// FileInfo represents information about a single file or symlink
type FileInfo struct {
	Path       string    `json:"path"`
	Hash       string    `json:"hash"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	IsSymlink  bool      `json:"is_symlink,omitempty"`
	LinkTarget string    `json:"link_target,omitempty"`
}

// Result represents the result of hash calculation
type Result struct {
	Files     []FileInfo `json:"files"`
	FileCount int        `json:"file_count"`
}

// Calculator handles hash calculation for files and directories
type Calculator struct {
	numWorkers        int
	bufferSize        int
	bytesPerSec       int64                   // Rate limit in bytes per second (0 = no limit)
	limiter           *rate.Limiter           // Shared rate limiter for all workers
	metadataCache     *cache.MetadataVerifier // Optional metadata cache for fast verification
	verifyProbability float64                 // Probability of hash verification (0.0-1.0)
	manifestHashes    map[string]string       // Optional manifest hashes for cache-based verification
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

// EnableMetadataCache enables metadata caching for fast verification
func (c *Calculator) EnableMetadataCache(cacheDir, targetDir, baseName, appName string, manifestTime time.Time) error {
	c.metadataCache = cache.NewMetadataVerifier(cacheDir, targetDir, baseName, appName)
	if err := c.metadataCache.Load(); err != nil {
		// Log warning but continue (cache will be rebuilt)
		fmt.Fprintf(os.Stderr, "Warning: failed to load cache: %v\n", err)
	}
	// Set manifest time and check validity
	c.metadataCache.SetManifestTime(manifestTime)
	if !c.metadataCache.IsValidForManifest(manifestTime) {
		// Cache is older than manifest, clear it
		c.metadataCache.Clear()
		fmt.Fprintf(os.Stderr, "Info: Cache cleared as it's older than manifest\n")
	}
	return nil
}

// SetVerifyProbability sets the probability of hash verification (0.0-1.0)
// 0.0 = always use cache if available (fastest)
// 1.0 = always calculate hash (most secure)
// 0.1 = 10% chance to verify hash even if cache hit
func (c *Calculator) SetVerifyProbability(p float64) {
	if p < 0 {
		p = 0
	} else if p > 1 {
		p = 1
	}
	c.verifyProbability = p
}

// SetManifestHashes sets the manifest hashes for cache-based verification
func (c *Calculator) SetManifestHashes(hashes map[string]string) {
	c.manifestHashes = hashes
}

// UpdateCacheForFiles updates cache entries for all provided files
func (c *Calculator) UpdateCacheForFiles(rootDir string, files []FileInfo) error {
	if c.metadataCache == nil {
		return nil
	}

	for _, file := range files {
		// Only update cache for regular files (not symlinks)
		if !file.IsSymlink {
			// Convert relative path back to absolute path
			absPath := filepath.Join(rootDir, file.Path)
			if err := c.metadataCache.UpdateMetadata(absPath); err != nil {
				// Log warning but continue with other files
				fmt.Fprintf(os.Stderr, "Warning: failed to update cache for %s: %v\n", file.Path, err)
			}
		}
	}

	return nil
}

// SaveMetadataCache saves the current metadata cache to disk
func (c *Calculator) SaveMetadataCache() error {
	if c.metadataCache == nil {
		return nil
	}
	return c.metadataCache.Save()
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

	return &Result{
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

		// Get relative path for checking exclude patterns
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}

		// Normalize path (use forward slash even on Windows)
		relPath = filepath.ToSlash(relPath)

		// For directories, check if they should be skipped entirely
		if info.IsDir() {
			// Check if this directory matches exclude patterns
			if matchExcludePatterns(relPath, excludes) {
				return filepath.SkipDir // Skip entire directory tree
			}
			// Also check if this directory could contain excluded subdirectories
			// For patterns like "logs/**", we want to skip the "logs" directory entirely
			if shouldSkipDirectory(relPath, excludes) {
				return filepath.SkipDir
			}
			return nil // Continue into this directory
		}

		// For files, check exclude patterns
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

					var fileHash string
					needHashCalculation := true

					// Check cache if available (not for symlinks)
					if c.metadataCache != nil && info.Mode()&os.ModeSymlink == 0 {
						if c.metadataCache.CheckMetadata(path) {
							// Metadata matches - decide whether to verify based on probability
							if c.verifyProbability == 0 || rand.Float64() > c.verifyProbability {
								// Skip hash calculation, use manifest hash if available
								if c.manifestHashes != nil {
									if manifestHash, ok := c.manifestHashes[relPath]; ok {
										fileHash = manifestHash
										needHashCalculation = false
									}
								} else {
									// No manifest hashes, skip calculation anyway
									needHashCalculation = false
								}
							}
							// else: probabilistically verify even with cache hit
						}
					}

					// Handle symlinks or calculate hash if needed
					if needHashCalculation && info.Mode()&os.ModeSymlink != 0 {
						target, err := os.Readlink(path)
						if err != nil {
							errors <- fmt.Errorf("failed to read symlink %s: %w", path, err)
							continue
						}

						// Create a hash based on the symlink target path
						// This ensures changes to symlink targets are detected
						hasher.Reset()
						hasher.Write([]byte("symlink:" + target))
						fileHash = hex.EncodeToString(hasher.Sum(nil))
					} else if needHashCalculation {
						// Regular file - calculate hash
						var err error
						fileHash, err = c.hashFileWithHasher(ctx, path, hasher, buf)
						if err != nil {
							errors <- fmt.Errorf("failed to hash %s: %w", path, err)
							continue
						}

					}

					// Create result
					results <- FileInfo{
						Path:      relPath,
						Hash:      fileHash,
						Size:      info.Size(),
						ModTime:   info.ModTime(),
						IsSymlink: info.Mode()&os.ModeSymlink != 0,
						LinkTarget: func() string {
							if info.Mode()&os.ModeSymlink != 0 {
								target, _ := os.Readlink(path)
								return target
							}
							return ""
						}(),
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

// matchExcludePatterns checks if a path matches exclude patterns
func matchExcludePatterns(path string, excludes []string) bool {
	for _, pattern := range excludes {
		if matched := matchGlob(pattern, path); matched {
			return true
		}
	}
	return false
}

// shouldSkipDirectory checks if a directory should be skipped based on exclude patterns
// This optimizes performance by skipping entire directory trees early
func shouldSkipDirectory(dirPath string, excludes []string) bool {
	for _, pattern := range excludes {
		// Check for patterns that would match everything under this directory
		// Examples:
		// - "logs/**" should skip "logs" directory entirely
		// - "cache/**" should skip "cache" directory entirely
		// - "**/logs/**" should skip any "logs" directory at any level

		// Pattern ends with /** - check if directory matches the prefix
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")

			// Handle ** prefix patterns like "**/logs"
			if strings.HasPrefix(prefix, "**/") {
				dirName := strings.TrimPrefix(prefix, "**/")
				if dirPath == dirName || strings.HasSuffix(dirPath, "/"+dirName) {
					return true
				}
			} else if dirPath == prefix {
				// Exact directory match
				return true
			}
		}

		// Pattern is just ** - matches everything
		if pattern == "**" || pattern == "**/*" {
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

	// Compare file hashes
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

	// Compare file hashes
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
