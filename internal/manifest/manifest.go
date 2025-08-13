package manifest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/catatsuy/kekkai/internal/hash"
)

// Manifest represents the complete manifest structure
type Manifest struct {
	Version     string          `json:"version"`
	TotalHash   string          `json:"total_hash"`
	FileCount   int             `json:"file_count"`
	GeneratedAt string          `json:"generated_at"`
	Excludes    []string        `json:"excludes,omitempty"`
	Files       []hash.FileInfo `json:"files"`
}

// Generator handles manifest generation
type Generator struct {
	calculator *hash.Calculator
}

// NewGenerator creates a new manifest generator
func NewGenerator() *Generator {
	return &Generator{
		calculator: hash.NewCalculator(),
	}
}

// Generate creates a manifest for the specified directory
func (g *Generator) Generate(targetDir string, excludes []string) (*Manifest, error) {
	// Calculate hashes
	result, err := g.calculator.CalculateDirectory(targetDir, excludes)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate directory hash: %w", err)
	}

	// Create manifest
	manifest := &Manifest{
		Version:     "1.0",
		TotalHash:   result.TotalHash,
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

// Verify checks the integrity of files against this manifest
func (m *Manifest) Verify(targetDir string) error {
	// Convert to hash.Result for verification
	result := &hash.Result{
		TotalHash: m.TotalHash,
		Files:     m.Files,
		FileCount: m.FileCount,
	}

	return hash.VerifyIntegrityWithPatterns(result, targetDir, m.Excludes)
}

// GetSummary returns a summary of the manifest
func (m *Manifest) GetSummary() string {
	return fmt.Sprintf(
		"Version: %s\nGenerated: %s\nTotal Hash: %s\nFile Count: %d",
		m.Version,
		m.GeneratedAt,
		m.TotalHash,
		m.FileCount,
	)
}
