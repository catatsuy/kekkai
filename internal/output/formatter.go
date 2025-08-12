package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// VerificationResult represents the result of a verification
type VerificationResult struct {
	Success   bool                 `json:"success"`
	Timestamp string               `json:"timestamp"`
	Message   string               `json:"message,omitempty"`
	Error     string               `json:"error,omitempty"`
	Details   *VerificationDetails `json:"details,omitempty"`
}

// VerificationDetails contains detailed verification information
type VerificationDetails struct {
	TotalFiles    int      `json:"total_files"`
	VerifiedFiles int      `json:"verified_files"`
	ModifiedFiles []string `json:"modified_files,omitempty"`
	DeletedFiles  []string `json:"deleted_files,omitempty"`
	AddedFiles    []string `json:"added_files,omitempty"`
}

// Formatter handles output formatting
type Formatter struct {
	writer io.Writer
}

// NewFormatter creates a new formatter
func NewFormatter(w io.Writer) *Formatter {
	return &Formatter{
		writer: w,
	}
}

// Format formats the verification result based on the specified format
func (f *Formatter) Format(result *VerificationResult, format string) error {
	switch format {
	case "json":
		return f.formatJSON(result)
	case "text":
		return f.formatText(result)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// formatJSON outputs in JSON format
func (f *Formatter) formatJSON(result *VerificationResult) error {
	encoder := json.NewEncoder(f.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// formatText outputs in human-readable text format
func (f *Formatter) formatText(result *VerificationResult) error {
	if result.Success {
		_, err := fmt.Fprintln(f.writer, "✓ Integrity check passed")
		if result.Details != nil {
			fmt.Fprintf(f.writer, "  Verified %d files\n", result.Details.VerifiedFiles)
		}
		return err
	}

	_, err := fmt.Fprintln(f.writer, "✗ Integrity check failed")
	if result.Error != "" {
		fmt.Fprintf(f.writer, "  Error: %s\n", result.Error)
	}

	if result.Details != nil {
		if len(result.Details.ModifiedFiles) > 0 {
			fmt.Fprintf(f.writer, "\n  Modified files (%d):\n", len(result.Details.ModifiedFiles))
			for _, file := range result.Details.ModifiedFiles {
				fmt.Fprintf(f.writer, "    - %s\n", file)
			}
		}

		if len(result.Details.DeletedFiles) > 0 {
			fmt.Fprintf(f.writer, "\n  Deleted files (%d):\n", len(result.Details.DeletedFiles))
			for _, file := range result.Details.DeletedFiles {
				fmt.Fprintf(f.writer, "    - %s\n", file)
			}
		}

		if len(result.Details.AddedFiles) > 0 {
			fmt.Fprintf(f.writer, "\n  Added files (%d):\n", len(result.Details.AddedFiles))
			for _, file := range result.Details.AddedFiles {
				fmt.Fprintf(f.writer, "    - %s\n", file)
			}
		}
	}

	return err
}

// GenerationResult represents the result of manifest generation
type GenerationResult struct {
	Success    bool   `json:"success"`
	Timestamp  string `json:"timestamp"`
	TotalHash  string `json:"total_hash"`
	FileCount  int    `json:"file_count"`
	OutputPath string `json:"output_path,omitempty"`
	S3Key      string `json:"s3_key,omitempty"`
	Error      string `json:"error,omitempty"`
}

// FormatGeneration formats the generation result
func (f *Formatter) FormatGeneration(result *GenerationResult, format string) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(f.writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	case "text":
		if result.Success {
			fmt.Fprintln(f.writer, "✓ Manifest generated successfully")
			fmt.Fprintf(f.writer, "  Total Hash: %s\n", result.TotalHash)
			fmt.Fprintf(f.writer, "  File Count: %d\n", result.FileCount)
			if result.OutputPath != "" {
				fmt.Fprintf(f.writer, "  Output: %s\n", result.OutputPath)
			}
			if result.S3Key != "" {
				fmt.Fprintf(f.writer, "  S3 Key: %s\n", result.S3Key)
			}
		} else {
			fmt.Fprintln(f.writer, "✗ Failed to generate manifest")
			if result.Error != "" {
				fmt.Fprintf(f.writer, "  Error: %s\n", result.Error)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// ParseVerificationError parses an error from verification and extracts details
func ParseVerificationError(err error) *VerificationDetails {
	if err == nil {
		return nil
	}

	// Parse error message to extract file changes
	details := &VerificationDetails{
		ModifiedFiles: []string{},
		DeletedFiles:  []string{},
		AddedFiles:    []string{},
	}

	// Parse error message to extract file changes
	errStr := err.Error()
	lines := strings.Split(errStr, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "modified:") {
			file := strings.TrimSpace(strings.TrimPrefix(line, "modified:"))
			details.ModifiedFiles = append(details.ModifiedFiles, file)
		} else if strings.HasPrefix(line, "deleted:") {
			file := strings.TrimSpace(strings.TrimPrefix(line, "deleted:"))
			details.DeletedFiles = append(details.DeletedFiles, file)
		} else if strings.HasPrefix(line, "added:") {
			file := strings.TrimSpace(strings.TrimPrefix(line, "added:"))
			details.AddedFiles = append(details.AddedFiles, file)
		}
	}

	return details
}
