package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// MetadataCache represents cached file metadata for verification
type MetadataCache struct {
	Version         string                   `json:"version"`
	CreatedAt       time.Time                `json:"created_at"`
	ManifestGenTime time.Time                `json:"manifest_gen_time"` // Time when manifest was generated
	CacheHash       string                   `json:"cache_hash"`        // Hash of the cache file itself
	Files           map[string]MetadataEntry `json:"files"`
}

// MetadataEntry represents cached metadata for a single file
type MetadataEntry struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	CTime   time.Time `json:"ctime"` // Change time (metadata change)
}

// MetadataVerifier manages metadata verification cache
type MetadataVerifier struct {
	cachePath string
	data      *MetadataCache
	mu        sync.RWMutex
	hitCount  int64 // Cache hit statistics (accessed atomically)
	missCount int64 // Cache miss statistics (accessed atomically)
}

// NewMetadataVerifier creates a new metadata cache instance
func NewMetadataVerifier(cacheDir, targetDir, baseName, appName string) *MetadataVerifier {
	// Create cache filename with app-name and base-name (no target hash)
	cachePath := filepath.Join(cacheDir, fmt.Sprintf(".kekkai-cache-%s-%s.json", baseName, appName))
	return &MetadataVerifier{
		cachePath: cachePath,
	}
}

// Load reads the cache from disk
func (v *MetadataVerifier) Load() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	data, err := os.ReadFile(v.cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize empty cache
			v.data = &MetadataCache{
				Version:   "2.0",
				CreatedAt: time.Now(),
				Files:     make(map[string]MetadataEntry),
			}
			return nil
		}
		return fmt.Errorf("failed to read cache: %w", err)
	}

	var cache MetadataCache
	if err := json.Unmarshal(data, &cache); err != nil {
		// Cache is corrupted, start fresh
		v.data = &MetadataCache{
			Version:   "2.0",
			CreatedAt: time.Now(),
			Files:     make(map[string]MetadataEntry),
		}
		return fmt.Errorf("failed to parse cache: %w", err)
	}

	// Verify cache integrity
	if !v.verifyCacheIntegrity(&cache) {
		// Cache is corrupted or tampered, start fresh
		v.data = &MetadataCache{
			Version:   "2.0",
			CreatedAt: time.Now(),
			Files:     make(map[string]MetadataEntry),
		}
		return fmt.Errorf("cache integrity check failed, starting fresh")
	}

	v.data = &cache
	return nil
}

// SetManifestTime sets the manifest generation time for cache validity check
func (v *MetadataVerifier) SetManifestTime(t time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.data != nil {
		v.data.ManifestGenTime = t
	}
}

// IsValidForManifest checks if cache is valid for the given manifest time
func (v *MetadataVerifier) IsValidForManifest(manifestTime time.Time) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.data == nil {
		return false
	}
	// Cache is valid if it was created after the manifest
	return v.data.CreatedAt.After(manifestTime) || v.data.CreatedAt.Equal(manifestTime)
}

// CheckMetadata checks if a file's metadata matches the cache
func (v *MetadataVerifier) CheckMetadata(path string) (metadataMatches bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.data == nil {
		return false
	}

	entry, exists := v.data.Files[path]
	if !exists {
		atomic.AddInt64(&v.missCount, 1)
		return false
	}

	// Get current file stats
	info, err := os.Lstat(path)
	if err != nil {
		atomic.AddInt64(&v.missCount, 1)
		return false
	}

	// Get system-specific stats
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		atomic.AddInt64(&v.missCount, 1)
		return false
	}

	// Get ctime from system stats
	ctime := getCtime(stat)

	// Check all metadata
	if info.Size() != entry.Size {
		atomic.AddInt64(&v.missCount, 1)
		return false
	}

	if !info.ModTime().Equal(entry.ModTime) {
		atomic.AddInt64(&v.missCount, 1)
		return false
	}

	// ctime is the most important - it can't be easily forged
	// Use platform-specific comparison to handle filesystem timestamp precision issues
	if !isTimeEqualPlatform(ctime, entry.CTime) {
		atomic.AddInt64(&v.missCount, 1)
		return false
	}

	// All metadata matches
	atomic.AddInt64(&v.hitCount, 1)
	return true
}

// UpdateMetadata updates the cache entry for a file's metadata
func (v *MetadataVerifier) UpdateMetadata(path string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.data == nil {
		v.data = &MetadataCache{
			Version:   "2.0",
			CreatedAt: time.Now(),
			Files:     make(map[string]MetadataEntry),
		}
	}

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("failed to get system stats")
	}

	ctime := getCtime(stat)

	v.data.Files[path] = MetadataEntry{
		Path:    path,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		CTime:   ctime,
	}

	return nil
}

// Save writes the cache to disk
func (v *MetadataVerifier) Save() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.data == nil {
		return fmt.Errorf("no cache data to save")
	}

	// Clone data for hash calculation
	tempCache := *v.data
	tempCache.CacheHash = "" // Clear hash for calculation

	// Calculate cache hash
	tempData, err := json.Marshal(tempCache)
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	hasher := sha256.New()
	hasher.Write(tempData)
	v.data.CacheHash = hex.EncodeToString(hasher.Sum(nil))

	// Marshal final data with hash
	finalData, err := json.MarshalIndent(v.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache with hash: %w", err)
	}

	// Write atomically using rename
	tempPath := v.cachePath + ".tmp"
	if err := os.WriteFile(tempPath, finalData, 0644); err != nil {
		return fmt.Errorf("failed to write cache: %w", err)
	}

	if err := os.Rename(tempPath, v.cachePath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to save cache: %w", err)
	}

	return nil
}

// Clear removes all cache entries
func (v *MetadataVerifier) Clear() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.data = &MetadataCache{
		Version:   "2.0",
		CreatedAt: time.Now(),
		Files:     make(map[string]MetadataEntry),
	}
}

// Remove deletes the cache file
func (v *MetadataVerifier) Remove() error {
	return os.Remove(v.cachePath)
}

// verifyCacheIntegrity checks if the cache file has been tampered with
func (v *MetadataVerifier) verifyCacheIntegrity(cache *MetadataCache) bool {
	if cache == nil || cache.CacheHash == "" {
		// No hash to verify
		return true // Allow empty cache
	}

	// Store and clear hash for verification
	expectedHash := cache.CacheHash
	tempCache := *cache
	tempCache.CacheHash = ""

	// Recalculate hash
	data, err := json.Marshal(tempCache)
	if err != nil {
		return false
	}

	hasher := sha256.New()
	hasher.Write(data)
	actualHash := hex.EncodeToString(hasher.Sum(nil))

	return actualHash == expectedHash
}

// GetStats returns cache hit/miss statistics
func (v *MetadataVerifier) GetStats() (hits, misses int64) {
	return atomic.LoadInt64(&v.hitCount), atomic.LoadInt64(&v.missCount)
}

// ResetStats resets cache statistics
func (v *MetadataVerifier) ResetStats() {
	atomic.StoreInt64(&v.hitCount, 0)
	atomic.StoreInt64(&v.missCount, 0)
}
