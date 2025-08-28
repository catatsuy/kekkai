package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/catatsuy/kekkai/internal/hash"
)

// Manifest represents the complete manifest structure
type Manifest struct {
	Version     string          `json:"version"`
	FileCount   int             `json:"file_count"`
	GeneratedAt string          `json:"generated_at"`
	Excludes    []string        `json:"excludes,omitempty"`
	Files       []hash.FileInfo `json:"files"`
}

// Generator handles manifest generation
type Generator struct {
	calculator *hash.Calculator
}

// NewGenerator creates a manifest generator with custom worker count
func NewGenerator(numWorkers int) *Generator {
	return &Generator{
		calculator: hash.NewCalculator(numWorkers),
	}
}

// NewGeneratorWithRateLimit creates a manifest generator with rate limiting
func NewGeneratorWithRateLimit(numWorkers int, bytesPerSec int64) *Generator {
	return &Generator{
		calculator: hash.NewCalculatorWithRateLimit(numWorkers, bytesPerSec),
	}
}

// Generate creates a manifest for the specified directory with context
func (g *Generator) Generate(ctx context.Context, targetDir string, excludes []string) (*Manifest, error) {
	// Calculate hashes
	result, err := g.calculator.CalculateDirectory(ctx, targetDir, excludes)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate directory hash: %w", err)
	}

	// Create manifest
	manifest := &Manifest{
		Version:     "1.0",
		FileCount:   result.FileCount,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Excludes:    excludes,
		Files:       result.Files,
	}

	return manifest, nil
}

// SaveToFile saves the manifest to a file
func SaveToFile(manifest *Manifest, filename string) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write manifest file: %w", err)
	}

	return nil
}

// SaveToWriter saves the manifest to an io.Writer
func SaveToWriter(manifest *Manifest, w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(manifest); err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}

	return nil
}

// LoadFromFile loads a manifest from a file
func LoadFromFile(filename string) (*Manifest, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	var manifest Manifest
	err = json.Unmarshal(data, &manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	return &manifest, nil
}

// LoadFromReader loads a manifest from an io.Reader
func LoadFromReader(r io.Reader) (*Manifest, error) {
	var manifest Manifest

	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	return &manifest, nil
}

// Verify checks the integrity of files with context
func (m *Manifest) Verify(ctx context.Context, targetDir string, numWorkers int) error {
	calculator := hash.NewCalculator(numWorkers)
	return m.verifyWithCalculator(ctx, targetDir, calculator)
}

// VerifyWithRateLimit checks the integrity of files with rate limiting and context
func (m *Manifest) VerifyWithRateLimit(ctx context.Context, targetDir string, numWorkers int, bytesPerSec int64) error {
	calculator := hash.NewCalculatorWithRateLimit(numWorkers, bytesPerSec)
	return m.verifyWithCalculator(ctx, targetDir, calculator)
}

// VerifyWithCache checks integrity using cache with probabilistic verification
func (m *Manifest) VerifyWithCache(ctx context.Context, targetDir, cacheDir, baseName, appName string, numWorkers int, verifyProbability float64) error {
	calculator := hash.NewCalculator(numWorkers)
	// Enable cache for the specified directory
	manifestTime, _ := time.Parse(time.RFC3339, m.GeneratedAt)
	if err := calculator.EnableMetadataCache(cacheDir, targetDir, baseName, appName, manifestTime); err != nil {
		return fmt.Errorf("failed to enable cache: %w", err)
	}
	calculator.SetVerifyProbability(verifyProbability)
	// Set manifest hashes for cache-based verification
	manifestHashes := make(map[string]string)
	for _, f := range m.Files {
		manifestHashes[f.Path] = f.Hash
	}
	calculator.SetManifestHashes(manifestHashes)

	// Perform verification
	err := m.verifyWithCalculator(ctx, targetDir, calculator)

	// Only update cache if verification was successful
	if err == nil {
		calculator.UpdateCacheForFiles(targetDir, m.Files)
		calculator.SaveMetadataCache()
	}

	return err
}

// VerifyWithCacheAndRateLimit combines cache verification with rate limiting
func (m *Manifest) VerifyWithCacheAndRateLimit(ctx context.Context, targetDir, cacheDir, baseName, appName string, numWorkers int, bytesPerSec int64, verifyProbability float64) error {
	calculator := hash.NewCalculatorWithRateLimit(numWorkers, bytesPerSec)
	// Enable cache for the specified directory
	manifestTime, _ := time.Parse(time.RFC3339, m.GeneratedAt)
	if err := calculator.EnableMetadataCache(cacheDir, targetDir, baseName, appName, manifestTime); err != nil {
		return fmt.Errorf("failed to enable cache: %w", err)
	}
	calculator.SetVerifyProbability(verifyProbability)
	// Set manifest hashes for cache-based verification
	manifestHashes := make(map[string]string)
	for _, f := range m.Files {
		manifestHashes[f.Path] = f.Hash
	}
	calculator.SetManifestHashes(manifestHashes)

	// Perform verification
	err := m.verifyWithCalculator(ctx, targetDir, calculator)

	// Only update cache if verification was successful
	if err == nil {
		calculator.UpdateCacheForFiles(targetDir, m.Files)
		calculator.SaveMetadataCache()
	}

	return err
}

// verifyWithCalculator performs the actual verification with the provided calculator and context
func (m *Manifest) verifyWithCalculator(ctx context.Context, targetDir string, calculator *hash.Calculator) error {
	// Calculate current state with same patterns
	currentResult, err := calculator.CalculateDirectory(ctx, targetDir, m.Excludes)
	if err != nil {
		return fmt.Errorf("failed to calculate current state: %w", err)
	}

	// Compare file hashes
	manifestMap := make(map[string]hash.FileInfo)
	for _, f := range m.Files {
		manifestMap[f.Path] = f
	}

	currentMap := make(map[string]hash.FileInfo)
	for _, f := range currentResult.Files {
		currentMap[f.Path] = f
	}

	issues := make([]string, 0, 10)

	// Check for modified/deleted files (checking hash/size/type)
	for path, expectedFile := range manifestMap {
		if actualFile, exists := currentMap[path]; exists {
			// Check file type (symlink vs regular file)
			if expectedFile.IsSymlink != actualFile.IsSymlink {
				// Use modified: prefix for CLI compatibility
				issues = append(issues, fmt.Sprintf(
					"modified: %s (type %s→%s)",
					path,
					func() string {
						if expectedFile.IsSymlink {
							return "symlink"
						}
						return "file"
					}(),
					func() string {
						if actualFile.IsSymlink {
							return "symlink"
						}
						return "file"
					}(),
				))
				continue
			}
			// Check content hash
			if expectedFile.Hash != actualFile.Hash {
				issues = append(issues, fmt.Sprintf("modified: %s (hash)", path))
				continue
			}
			// Check size (for both symlinks and regular files for consistency with totalHash)
			if expectedFile.Size != actualFile.Size {
				issues = append(issues, fmt.Sprintf(
					"modified: %s (size %d→%d)", path, expectedFile.Size, actualFile.Size))
				continue
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

// GetSummary returns a summary of the manifest
func (m *Manifest) GetSummary() string {
	return fmt.Sprintf(
		"Version: %s\nGenerated: %s\nFile Count: %d",
		m.Version,
		m.GeneratedAt,
		m.FileCount,
	)
}
