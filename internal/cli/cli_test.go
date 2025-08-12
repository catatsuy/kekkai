package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cli := NewCLI(&stdout, &stderr)

	tests := []struct {
		name     string
		args     []string
		wantExit int
		wantOut  string
	}{
		{
			name:     "version command",
			args:     []string{"kekkai", "version"},
			wantExit: ExitCodeOK,
			wantOut:  "kekkai version",
		},
		{
			name:     "version flag",
			args:     []string{"kekkai", "--version"},
			wantExit: ExitCodeOK,
			wantOut:  "kekkai version",
		},
		{
			name:     "short version flag",
			args:     []string{"kekkai", "-v"},
			wantExit: ExitCodeOK,
			wantOut:  "kekkai version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()

			exitCode := cli.Run(tt.args)

			if exitCode != tt.wantExit {
				t.Errorf("Run() exit code = %v, want %v", exitCode, tt.wantExit)
			}

			output := stdout.String()
			if !strings.Contains(output, tt.wantOut) {
				t.Errorf("Output should contain '%s', got: %s", tt.wantOut, output)
			}
		})
	}
}

func TestCLIHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cli := NewCLI(&stdout, &stderr)

	tests := []struct {
		name     string
		args     []string
		wantExit int
		contains []string
	}{
		{
			name:     "help command",
			args:     []string{"kekkai", "help"},
			wantExit: ExitCodeOK,
			contains: []string{"Usage:", "generate", "verify"},
		},
		{
			name:     "help flag",
			args:     []string{"kekkai", "--help"},
			wantExit: ExitCodeOK,
			contains: []string{"Usage:", "generate", "verify"},
		},
		{
			name:     "no arguments",
			args:     []string{"kekkai"},
			wantExit: ExitCodeFail,
			contains: []string{"Usage:"},
		},
		{
			name:     "generate help",
			args:     []string{"kekkai", "generate", "--help"},
			wantExit: ExitCodeOK,
			contains: []string{"generate", "target", "output", "include", "exclude"},
		},
		{
			name:     "verify help",
			args:     []string{"kekkai", "verify", "--help"},
			wantExit: ExitCodeOK,
			contains: []string{"verify", "manifest", "target", "format"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()

			exitCode := cli.Run(tt.args)

			if exitCode != tt.wantExit {
				t.Errorf("Run() exit code = %v, want %v", exitCode, tt.wantExit)
			}

			output := stderr.String()
			for _, want := range tt.contains {
				if !strings.Contains(output, want) {
					t.Errorf("Output should contain '%s', got: %s", want, output)
				}
			}
		})
	}
}

func TestCLIGenerate(t *testing.T) {
	// Create test directory
	tempDir := t.TempDir()

	// Create test files
	testFiles := map[string]string{
		"test.txt":  "test content",
		"index.php": "<?php echo 'hello';",
		"script.js": "console.log('test');",
		"error.log": "error message",
	}

	for name, content := range testFiles {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name     string
		args     []string
		wantExit int
		check    func(t *testing.T, stdout, stderr string)
	}{
		{
			name:     "generate to stdout",
			args:     []string{"kekkai", "generate", "--target", tempDir},
			wantExit: ExitCodeOK,
			check: func(t *testing.T, stdout, stderr string) {
				// Check JSON output contains expected fields
				if !strings.Contains(stdout, `"version"`) {
					t.Error("Output should contain version field")
				}
				if !strings.Contains(stdout, `"total_hash"`) {
					t.Error("Output should contain total_hash field")
				}
				if !strings.Contains(stdout, `"files"`) {
					t.Error("Output should contain files field")
				}
			},
		},
		{
			name: "generate with includes",
			args: []string{"kekkai", "generate",
				"--target", tempDir,
				"--include", "*.php",
				"--include", "*.txt",
			},
			wantExit: ExitCodeOK,
			check: func(t *testing.T, stdout, stderr string) {
				if !strings.Contains(stdout, "test.txt") {
					t.Error("Output should include test.txt")
				}
				if !strings.Contains(stdout, "index.php") {
					t.Error("Output should include index.php")
				}
				if strings.Contains(stdout, "script.js") {
					t.Error("Output should not include script.js")
				}
			},
		},
		{
			name: "generate with excludes",
			args: []string{"kekkai", "generate",
				"--target", tempDir,
				"--exclude", "*.log",
			},
			wantExit: ExitCodeOK,
			check: func(t *testing.T, stdout, stderr string) {
				if strings.Contains(stdout, "error.log") {
					t.Error("Output should not include error.log")
				}
			},
		},
		{
			name: "generate to file",
			args: []string{"kekkai", "generate",
				"--target", tempDir,
				"--output", filepath.Join(tempDir, "manifest.json"),
				"--format", "text",
			},
			wantExit: ExitCodeOK,
			check: func(t *testing.T, stdout, stderr string) {
				// Check success message
				if !strings.Contains(stdout, "Integrity check passed") &&
					!strings.Contains(stdout, "successfully") {
					t.Error("Should show success message")
				}

				// Check file was created
				manifestPath := filepath.Join(tempDir, "manifest.json")
				if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
					t.Error("Manifest file should be created")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cli := NewCLI(&stdout, &stderr)

			exitCode := cli.Run(tt.args)

			if exitCode != tt.wantExit {
				t.Errorf("Run() exit code = %v, want %v\nstderr: %s",
					exitCode, tt.wantExit, stderr.String())
			}

			if tt.check != nil {
				tt.check(t, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCLIVerify(t *testing.T) {
	// Create test directory
	tempDir := t.TempDir()

	// Create test files
	testFiles := map[string]string{
		"test.txt":  "test content",
		"index.php": "<?php echo 'hello';",
	}

	for name, content := range testFiles {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Generate manifest first
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	var stdout, stderr bytes.Buffer
	cli := NewCLI(&stdout, &stderr)

	generateArgs := []string{"kekkai", "generate",
		"--target", tempDir,
		"--output", manifestPath,
		"--exclude", "manifest.json",
	}

	if exitCode := cli.Run(generateArgs); exitCode != ExitCodeOK {
		t.Fatalf("Failed to generate manifest: exit code %d", exitCode)
	}

	tests := []struct {
		name     string
		args     []string
		wantExit int
		modifyFn func() // Function to modify files before verify
		checkOut func(t *testing.T, stdout, stderr string)
	}{
		{
			name: "verify success",
			args: []string{"kekkai", "verify",
				"--manifest", manifestPath,
				"--target", tempDir,
			},
			wantExit: ExitCodeOK,
			checkOut: func(t *testing.T, stdout, stderr string) {
				if !strings.Contains(stdout, "passed") &&
					!strings.Contains(stdout, "successfully") {
					t.Errorf("Should show success message, got stdout=%q stderr=%q", stdout, stderr)
				}
			},
		},
		{
			name: "verify with modified file",
			args: []string{"kekkai", "verify",
				"--manifest", manifestPath,
				"--target", tempDir,
			},
			modifyFn: func() {
				// Modify a file
				path := filepath.Join(tempDir, "test.txt")
				os.WriteFile(path, []byte("modified"), 0644)
			},
			wantExit: ExitCodeFail,
			checkOut: func(t *testing.T, stdout, stderr string) {
				if !strings.Contains(stderr, "failed") {
					t.Error("Should show failure message")
				}
			},
		},
		{
			name: "verify with JSON format",
			args: []string{"kekkai", "verify",
				"--manifest", manifestPath,
				"--target", tempDir,
				"--format", "json",
			},
			wantExit: ExitCodeOK,
			checkOut: func(t *testing.T, stdout, stderr string) {
				output := stdout + stderr
				if !strings.Contains(output, `"success"`) {
					t.Error("JSON output should contain success field")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset files
			for name, content := range testFiles {
				path := filepath.Join(tempDir, name)
				os.WriteFile(path, []byte(content), 0644)
			}

			if tt.modifyFn != nil {
				tt.modifyFn()
			}

			stdout.Reset()
			stderr.Reset()

			exitCode := cli.Run(tt.args)

			if exitCode != tt.wantExit {
				t.Errorf("Run() exit code = %v, want %v\nstderr: %s",
					exitCode, tt.wantExit, stderr.String())
			}

			if tt.checkOut != nil {
				tt.checkOut(t, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCLIVerifyWithExcludes(t *testing.T) {
	// Create test directory
	tempDir := t.TempDir()

	// Create test files
	testFiles := map[string]string{
		"app.txt":    "app content",
		"config.txt": "config content",
		"debug.log":  "debug log",
		"error.log":  "error log",
		"cache.tmp":  "cache temp",
	}

	for name, content := range testFiles {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Generate manifest with excludes
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	var stdout, stderr bytes.Buffer
	cli := NewCLI(&stdout, &stderr)

	generateArgs := []string{"kekkai", "generate",
		"--target", tempDir,
		"--output", manifestPath,
		"--exclude", "*.log",
		"--exclude", "*.tmp",
	}

	if exitCode := cli.Run(generateArgs); exitCode != ExitCodeOK {
		t.Fatalf("Failed to generate manifest: exit code %d", exitCode)
	}

	// Test cases
	tests := []struct {
		name     string
		modifyFn func()
		wantExit int
		checkMsg string
	}{
		{
			name:     "verify with no changes",
			wantExit: ExitCodeOK,
			checkMsg: "passed",
		},
		{
			name: "verify with modified excluded file",
			modifyFn: func() {
				// Modify an excluded log file
				path := filepath.Join(tempDir, "debug.log")
				os.WriteFile(path, []byte("modified log"), 0644)
			},
			wantExit: ExitCodeOK,
			checkMsg: "passed",
		},
		{
			name: "verify with added excluded file",
			modifyFn: func() {
				// Add a new log file
				path := filepath.Join(tempDir, "new.log")
				os.WriteFile(path, []byte("new log"), 0644)
			},
			wantExit: ExitCodeOK,
			checkMsg: "passed",
		},
		{
			name: "verify with modified included file",
			modifyFn: func() {
				// Modify an included file
				path := filepath.Join(tempDir, "app.txt")
				os.WriteFile(path, []byte("modified app"), 0644)
			},
			wantExit: ExitCodeFail,
			checkMsg: "modified: app.txt",
		},
		{
			name: "verify with added included file",
			modifyFn: func() {
				// Add a new txt file
				path := filepath.Join(tempDir, "new.txt")
				os.WriteFile(path, []byte("new file"), 0644)
			},
			wantExit: ExitCodeFail,
			checkMsg: "added: new.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset files to original state
			for name, content := range testFiles {
				path := filepath.Join(tempDir, name)
				os.WriteFile(path, []byte(content), 0644)
			}

			// Apply modifications if any
			if tt.modifyFn != nil {
				tt.modifyFn()
			}

			// Run verify
			stdout.Reset()
			stderr.Reset()

			verifyArgs := []string{"kekkai", "verify",
				"--manifest", manifestPath,
				"--target", tempDir,
			}

			exitCode := cli.Run(verifyArgs)

			if exitCode != tt.wantExit {
				t.Errorf("Run() exit code = %v, want %v", exitCode, tt.wantExit)
			}

			output := stdout.String() + stderr.String()
			if tt.checkMsg != "" && !strings.Contains(output, tt.checkMsg) {
				t.Errorf("Output should contain '%s', got: %s", tt.checkMsg, output)
			}

			// Clean up any added files
			files, _ := os.ReadDir(tempDir)
			for _, file := range files {
				if _, exists := testFiles[file.Name()]; !exists {
					os.Remove(filepath.Join(tempDir, file.Name()))
				}
			}
		})
	}
}

func TestCLIInvalidCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cli := NewCLI(&stdout, &stderr)

	tests := []struct {
		name     string
		args     []string
		wantExit int
		errMsg   string
	}{
		{
			name:     "unknown command",
			args:     []string{"kekkai", "unknown"},
			wantExit: ExitCodeFail,
			errMsg:   "Unknown command",
		},
		{
			name:     "missing manifest for verify",
			args:     []string{"kekkai", "verify", "--target", "."},
			wantExit: ExitCodeFail,
			errMsg:   "either -manifest or -s3-bucket must be specified",
		},
		{
			name: "s3 without key or app-name",
			args: []string{"kekkai", "generate",
				"--target", ".",
				"--s3-bucket", "test-bucket",
			},
			wantExit: ExitCodeFail,
			errMsg:   "Either -s3-key or -app-name must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()

			exitCode := cli.Run(tt.args)

			if exitCode != tt.wantExit {
				t.Errorf("Run() exit code = %v, want %v", exitCode, tt.wantExit)
			}

			errOutput := stderr.String()
			if !strings.Contains(errOutput, tt.errMsg) {
				t.Errorf("Error output should contain '%s', got: %s", tt.errMsg, errOutput)
			}
		})
	}
}

func TestArrayFlags(t *testing.T) {
	var flags arrayFlags

	// Test initial state
	if flags.String() != "" {
		t.Error("Initial String() should return empty string")
	}

	// Test Set
	flags.Set("*.php")
	flags.Set("*.js")

	if len(flags) != 2 {
		t.Errorf("Length = %d, want 2", len(flags))
	}

	// Test String
	str := flags.String()
	if str != "*.php,*.js" {
		t.Errorf("String() = %s, want '*.php,*.js'", str)
	}
}
