package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFormatVerificationResult(t *testing.T) {
	tests := []struct {
		name        string
		result      *VerificationResult
		format      string
		contains    []string
		notContains []string
	}{
		{
			name: "text format success",
			result: &VerificationResult{
				Success:   true,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Message:   "All files verified successfully",
				Details: &VerificationDetails{
					TotalFiles:    100,
					VerifiedFiles: 100,
				},
			},
			format:   "text",
			contains: []string{"✓", "passed", "100 files"},
		},
		{
			name: "text format failure",
			result: &VerificationResult{
				Success:   false,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     "integrity check failed",
				Details: &VerificationDetails{
					ModifiedFiles: []string{"file1.txt", "file2.txt"},
					DeletedFiles:  []string{"file3.txt"},
					AddedFiles:    []string{"file4.txt"},
				},
			},
			format:   "text",
			contains: []string{"✗", "failed", "Modified files", "file1.txt", "Deleted files", "Added files"},
		},
		{
			name: "json format success",
			result: &VerificationResult{
				Success:   true,
				Timestamp: "2024-01-01T00:00:00Z",
				Message:   "Test message",
			},
			format:   "json",
			contains: []string{`"success": true`, `"timestamp": "2024-01-01T00:00:00Z"`, `"message": "Test message"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			formatter := NewFormatter(&buf)

			err := formatter.Format(tt.result, tt.format)
			if err != nil {
				t.Fatalf("Format() error = %v", err)
			}

			output := buf.String()

			for _, want := range tt.contains {
				if !strings.Contains(output, want) {
					t.Errorf("Output should contain '%s', got: %s", want, output)
				}
			}

			for _, notWant := range tt.notContains {
				if strings.Contains(output, notWant) {
					t.Errorf("Output should not contain '%s', got: %s", notWant, output)
				}
			}
		})
	}
}

func TestFormatGenerationResult(t *testing.T) {
	tests := []struct {
		name     string
		result   *GenerationResult
		format   string
		contains []string
	}{
		{
			name: "text format success",
			result: &GenerationResult{
				Success:    true,
				Timestamp:  time.Now().UTC().Format(time.RFC3339),
				TotalHash:  "abc123def456",
				FileCount:  42,
				OutputPath: "/tmp/manifest.json",
			},
			format:   "text",
			contains: []string{"✓", "successfully", "abc123def456", "42", "/tmp/manifest.json"},
		},
		{
			name: "text format with S3",
			result: &GenerationResult{
				Success:   true,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				TotalHash: "xyz789",
				FileCount: 10,
				S3Key:     "production/app/manifest.json",
			},
			format:   "text",
			contains: []string{"✓", "S3 Key:", "production/app/manifest.json"},
		},
		{
			name: "text format failure",
			result: &GenerationResult{
				Success:   false,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     "failed to generate manifest",
			},
			format:   "text",
			contains: []string{"✗", "Failed", "failed to generate manifest"},
		},
		{
			name: "json format",
			result: &GenerationResult{
				Success:   true,
				Timestamp: "2024-01-01T00:00:00Z",
				TotalHash: "hash123",
				FileCount: 5,
			},
			format:   "json",
			contains: []string{`"success": true`, `"total_hash": "hash123"`, `"file_count": 5`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			formatter := NewFormatter(&buf)

			err := formatter.FormatGeneration(tt.result, tt.format)
			if err != nil {
				t.Fatalf("FormatGeneration() error = %v", err)
			}

			output := buf.String()

			for _, want := range tt.contains {
				if !strings.Contains(output, want) {
					t.Errorf("Output should contain '%s', got: %s", want, output)
				}
			}
		})
	}
}

func TestFormatUnsupportedFormat(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewFormatter(&buf)

	result := &VerificationResult{
		Success: true,
	}

	err := formatter.Format(result, "unsupported")
	if err == nil {
		t.Error("Expected error for unsupported format")
	}

	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("Error message should mention unsupported format, got: %v", err)
	}
}

func TestJSONMarshaling(t *testing.T) {
	// Test VerificationResult JSON
	t.Run("VerificationResult", func(t *testing.T) {
		result := &VerificationResult{
			Success:   true,
			Timestamp: "2024-01-01T00:00:00Z",
			Message:   "test",
			Details: &VerificationDetails{
				TotalFiles:    10,
				VerifiedFiles: 10,
				ModifiedFiles: []string{"file1.txt"},
			},
		}

		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}

		var unmarshaled VerificationResult
		err = json.Unmarshal(data, &unmarshaled)
		if err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}

		if unmarshaled.Success != result.Success {
			t.Error("Success field mismatch after unmarshaling")
		}

		if len(unmarshaled.Details.ModifiedFiles) != 1 {
			t.Error("ModifiedFiles should have 1 element")
		}
	})

	// Test GenerationResult JSON
	t.Run("GenerationResult", func(t *testing.T) {
		result := &GenerationResult{
			Success:    true,
			Timestamp:  "2024-01-01T00:00:00Z",
			TotalHash:  "abc123",
			FileCount:  5,
			OutputPath: "/tmp/manifest.json",
			S3Key:      "s3://bucket/key",
		}

		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}

		var unmarshaled GenerationResult
		err = json.Unmarshal(data, &unmarshaled)
		if err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}

		if unmarshaled.TotalHash != result.TotalHash {
			t.Error("TotalHash field mismatch after unmarshaling")
		}

		if unmarshaled.S3Key != result.S3Key {
			t.Error("S3Key field mismatch after unmarshaling")
		}
	})
}

func TestParseVerificationError(t *testing.T) {
	tests := []struct {
		name           string
		errorMsg       string
		expectModified int
		expectDeleted  int
		expectAdded    int
	}{
		{
			name: "parse modified files",
			errorMsg: `integrity check failed:
modified: file1.txt
modified: file2.txt
deleted: file3.txt
added: file4.txt`,
			expectModified: 2,
			expectDeleted:  1,
			expectAdded:    1,
		},
		{
			name:           "empty error",
			errorMsg:       "",
			expectModified: 0,
			expectDeleted:  0,
			expectAdded:    0,
		},
		{
			name: "only modified files",
			errorMsg: `modified: app.php
modified: config.php`,
			expectModified: 2,
			expectDeleted:  0,
			expectAdded:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := ParseVerificationError(fmt.Errorf("%s", tt.errorMsg))

			if details == nil {
				if tt.expectModified > 0 || tt.expectDeleted > 0 || tt.expectAdded > 0 {
					t.Fatal("Expected details but got nil")
				}
				return
			}

			if len(details.ModifiedFiles) != tt.expectModified {
				t.Errorf("ModifiedFiles count = %d, want %d",
					len(details.ModifiedFiles), tt.expectModified)
			}

			if len(details.DeletedFiles) != tt.expectDeleted {
				t.Errorf("DeletedFiles count = %d, want %d",
					len(details.DeletedFiles), tt.expectDeleted)
			}

			if len(details.AddedFiles) != tt.expectAdded {
				t.Errorf("AddedFiles count = %d, want %d",
					len(details.AddedFiles), tt.expectAdded)
			}
		})
	}
}
