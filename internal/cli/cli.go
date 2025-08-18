package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/catatsuy/kekkai/internal/manifest"
	"github.com/catatsuy/kekkai/internal/output"
	"github.com/catatsuy/kekkai/internal/storage"
)

const (
	ExitCodeOK   = 0
	ExitCodeFail = 1
)

var (
	Version string
)

// CLI holds the CLI application state
type CLI struct {
	outStream io.Writer
	errStream io.Writer

	appVersion string
}

// NewCLI creates a new CLI instance
func NewCLI(outStream, errStream io.Writer) *CLI {
	return &CLI{
		outStream:  outStream,
		errStream:  errStream,
		appVersion: version(),
	}
}

// version returns the application version
func version() string {
	if Version != "" {
		return Version
	}
	return "dev"
}

// arrayFlags allows multiple flag values
type arrayFlags []string

func (i *arrayFlags) String() string {
	return strings.Join(*i, ",")
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

// Run executes the CLI
func (c *CLI) Run(args []string) int {
	if len(args) <= 1 {
		c.printUsage()
		return ExitCodeFail
	}

	// Check for global commands first
	switch args[1] {
	case "version", "--version", "-v":
		fmt.Fprintf(c.outStream, "kekkai version %s; %s\n", c.appVersion, runtime.Version())
		return ExitCodeOK
	case "help", "--help", "-h":
		c.printUsage()
		return ExitCodeOK
	case "generate":
		return c.runGenerate(args)
	case "verify":
		return c.runVerify(args)
	default:
		fmt.Fprintf(c.errStream, "Error: Unknown command '%s'\n", args[1])
		c.printUsage()
		return ExitCodeFail
	}
}

// runGenerate handles the generate command
func (c *CLI) runGenerate(args []string) int {
	var (
		excludes arrayFlags

		target    string
		output    string
		s3Bucket  string
		s3Region  string
		basePath  string
		appName   string
		format    string
		workers   int
		rateLimit int64
		timeout   int
		help      bool
	)

	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	flags.SetOutput(c.errStream)

	flags.StringVar(&target, "target", ".", "Target directory to scan")
	flags.StringVar(&output, "output", "-", "Output file (- for stdout)")
	flags.StringVar(&s3Bucket, "s3-bucket", "", "S3 bucket for manifest storage")
	flags.StringVar(&s3Region, "s3-region", "", "AWS region (uses default if not specified)")
	flags.StringVar(&basePath, "base-path", "development", "Base path for S3 (e.g., production, staging, development)")
	flags.StringVar(&appName, "app-name", "", "Application name for S3 versioning")
	flags.StringVar(&format, "format", "text", "Output format (text|json)")
	flags.IntVar(&workers, "workers", 0, "Number of worker threads (0 = auto detect)")
	flags.Int64Var(&rateLimit, "rate-limit", 0, "Rate limit in bytes per second (0 = no limit)")
	flags.IntVar(&timeout, "timeout", 300, "Timeout in seconds (default: 300)")
	flags.BoolVar(&help, "help", false, "Show help for generate command")
	flags.BoolVar(&help, "h", false, "Show help for generate command")

	flags.Var(&excludes, "exclude", "Exclude pattern (can be specified multiple times)")

	err := flags.Parse(args[2:])
	if err != nil {
		return ExitCodeFail
	}

	if help {
		c.printGenerateHelp(flags)
		return ExitCodeOK
	}

	// Validate rate limit
	if rateLimit < 0 {
		fmt.Fprintf(c.errStream, "Error: rate-limit cannot be negative\n")
		return ExitCodeFail
	}
	if rateLimit > 0 && rateLimit < 1024 {
		fmt.Fprintf(c.errStream, "Warning: rate-limit %d is very low (< 1KB/s), this may be too restrictive\n", rateLimit)
	}

	// Create context with signal handling
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Apply timeout if specified
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	// Generate manifest
	var generator *manifest.Generator
	if rateLimit > 0 {
		generator = manifest.NewGeneratorWithRateLimit(workers, rateLimit)
	} else {
		generator = manifest.NewGenerator(workers)
	}

	m, err := generator.Generate(ctx, target, excludes)
	if err != nil {
		c.outputGenerateError(err, format)
		return ExitCodeFail
	}

	// Handle output
	var outputPath string
	var s3KeyUsed string

	if s3Bucket != "" {
		// Upload to S3
		s3Storage, err := storage.NewS3Storage(s3Bucket, s3Region)
		if err != nil {
			fmt.Fprintf(c.errStream, "Error: Failed to initialize S3: %v\n", err)
			return ExitCodeFail
		}

		if appName != "" {
			// Use versioning
			key, err := s3Storage.UploadWithVersioning(basePath, appName, m)
			if err == nil {
				s3KeyUsed = key
			}
		} else {
			fmt.Fprintf(c.errStream, "Error: -app-name must be specified with -s3-bucket\n")
			return ExitCodeFail
		}

		if err != nil {
			fmt.Fprintf(c.errStream, "Error: Failed to upload to S3: %v\n", err)
			return ExitCodeFail
		}
	} else if output == "-" {
		// Output to stdout
		err = manifest.SaveToWriter(m, c.outStream)
		if err != nil {
			fmt.Fprintf(c.errStream, "Error: Failed to write manifest: %v\n", err)
			return ExitCodeFail
		}
	} else {
		// Output to file
		err = manifest.SaveToFile(m, output)
		if err != nil {
			fmt.Fprintf(c.errStream, "Error: Failed to save manifest: %v\n", err)
			return ExitCodeFail
		}
		outputPath = output
	}

	// Format success result
	c.outputGenerateSuccess(m, outputPath, s3KeyUsed, format)

	return ExitCodeOK
}

// runVerify handles the verify command
func (c *CLI) runVerify(args []string) int {
	var (
		manifestPath      string
		s3Bucket          string
		s3Region          string
		basePath          string
		appName           string
		target            string
		format            string
		workers           int
		rateLimit         int64
		timeout           int
		useCache          bool
		cacheDir          string
		verifyProbability float64
		help              bool
	)

	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(c.errStream)

	flags.StringVar(&manifestPath, "manifest", "", "Path to manifest file")
	flags.StringVar(&s3Bucket, "s3-bucket", "", "S3 bucket for manifest")
	flags.StringVar(&s3Region, "s3-region", "", "AWS region (uses default if not specified)")
	flags.StringVar(&basePath, "base-path", "development", "Base path for S3 (e.g., production, staging, development)")
	flags.StringVar(&appName, "app-name", "", "Application name for S3")
	flags.StringVar(&target, "target", ".", "Target directory to verify")
	flags.StringVar(&format, "format", "text", "Output format (text|json)")
	flags.IntVar(&workers, "workers", 0, "Number of worker threads (0 = auto detect)")
	flags.Int64Var(&rateLimit, "rate-limit", 0, "Rate limit in bytes per second (0 = no limit)")
	flags.IntVar(&timeout, "timeout", 300, "Timeout in seconds (default: 300)")
	flags.BoolVar(&useCache, "use-cache", false, "Enable local cache for verification (checks size, mtime, ctime)")
	flags.StringVar(&cacheDir, "cache-dir", "", "Directory for cache file (default: system temp directory)")
	flags.Float64Var(&verifyProbability, "verify-probability", 0.1, "Probability of hash verification even with cache hit (0.0-1.0, default: 0.1)")
	flags.BoolVar(&help, "help", false, "Show help for verify command")
	flags.BoolVar(&help, "h", false, "Show help for verify command")

	err := flags.Parse(args[2:])
	if err != nil {
		return ExitCodeFail
	}

	if help {
		c.printVerifyHelp(flags)
		return ExitCodeOK
	}

	// Validate rate limit
	if rateLimit < 0 {
		fmt.Fprintf(c.errStream, "Error: rate-limit cannot be negative\n")
		return ExitCodeFail
	}
	if rateLimit > 0 && rateLimit < 1024 {
		fmt.Fprintf(c.errStream, "Warning: rate-limit %d is very low (< 1KB/s), this may be too restrictive\n", rateLimit)
	}

	// Load manifest
	var m *manifest.Manifest

	if s3Bucket != "" {
		// Load from S3
		s3Storage, err := storage.NewS3Storage(s3Bucket, s3Region)
		if err != nil {
			c.outputVerifyError(err, format)
			return ExitCodeFail
		}

		if appName != "" {
			// Load manifest
			m, err = s3Storage.DownloadManifest(basePath, appName)
		} else {
			err = fmt.Errorf("-app-name must be specified with -s3-bucket")
		}

		if err != nil {
			c.outputVerifyError(err, format)
			return ExitCodeFail
		}
	} else if manifestPath != "" {
		// Load from file
		m, err = manifest.LoadFromFile(manifestPath)
		if err != nil {
			c.outputVerifyError(err, format)
			return ExitCodeFail
		}
	} else {
		err := fmt.Errorf("either -manifest or -s3-bucket must be specified")
		c.outputVerifyError(err, format)
		return ExitCodeFail
	}

	// Create context with signal handling
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Apply timeout if specified
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	// Verify integrity
	if useCache {
		// Use cache directory (default to system temp directory if not specified)
		cacheDirToUse := cacheDir
		if cacheDirToUse == "" {
			cacheDirToUse = os.TempDir()
		}

		// Use cache with probabilistic verification
		if rateLimit > 0 {
			err = m.VerifyWithCacheAndRateLimit(ctx, target, cacheDirToUse, basePath, appName, workers, rateLimit, verifyProbability)
		} else {
			err = m.VerifyWithCache(ctx, target, cacheDirToUse, basePath, appName, workers, verifyProbability)
		}
	} else {
		// Normal verify mode: calculate all hashes
		if rateLimit > 0 {
			err = m.VerifyWithRateLimit(ctx, target, workers, rateLimit)
		} else {
			err = m.Verify(ctx, target, workers)
		}
	}

	// Output result
	c.outputVerifyResult(err, m, format)

	if err != nil {
		return ExitCodeFail
	}

	return ExitCodeOK
}

// Output helper functions
func (c *CLI) outputGenerateSuccess(m *manifest.Manifest, outputPath, s3Key, format string) {
	result := &output.GenerationResult{
		Success:    true,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		TotalHash:  m.TotalHash,
		FileCount:  m.FileCount,
		OutputPath: outputPath,
		S3Key:      s3Key,
	}

	formatter := output.NewFormatter(c.outStream)
	formatter.FormatGeneration(result, format)
}

func (c *CLI) outputGenerateError(err error, format string) {
	result := &output.GenerationResult{
		Success:   false,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Error:     err.Error(),
	}

	formatter := output.NewFormatter(c.errStream)
	formatter.FormatGeneration(result, format)
}

func (c *CLI) outputVerifyResult(err error, m *manifest.Manifest, format string) {
	result := &output.VerificationResult{
		Success:   err == nil,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	if err != nil {
		result.Error = err.Error()
		result.Details = parseVerificationError(err)
	} else {
		result.Message = "All files verified successfully"
		result.Details = &output.VerificationDetails{
			TotalFiles:    m.FileCount,
			VerifiedFiles: m.FileCount,
		}
	}

	var stream = c.outStream
	if !result.Success {
		stream = c.errStream
	}

	formatter := output.NewFormatter(stream)
	formatter.Format(result, format)
}

func (c *CLI) outputVerifyError(err error, format string) {
	result := &output.VerificationResult{
		Success:   false,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Error:     err.Error(),
	}

	formatter := output.NewFormatter(c.errStream)
	formatter.Format(result, format)
}

// parseVerificationError extracts details from verification errors
func parseVerificationError(err error) *output.VerificationDetails {
	if err == nil {
		return nil
	}

	errStr := err.Error()
	details := &output.VerificationDetails{
		ModifiedFiles: []string{},
		DeletedFiles:  []string{},
		AddedFiles:    []string{},
	}

	// Parse error message to extract file changes
	lines := strings.Split(errStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "modified:") {
			file := strings.TrimPrefix(line, "modified:")
			details.ModifiedFiles = append(details.ModifiedFiles, strings.TrimSpace(file))
		} else if strings.HasPrefix(line, "deleted:") {
			file := strings.TrimPrefix(line, "deleted:")
			details.DeletedFiles = append(details.DeletedFiles, strings.TrimSpace(file))
		} else if strings.HasPrefix(line, "added:") {
			file := strings.TrimPrefix(line, "added:")
			details.AddedFiles = append(details.AddedFiles, strings.TrimSpace(file))
		}
	}

	return details
}

// Help functions
func (c *CLI) printUsage() {
	fmt.Fprintf(c.errStream, `kekkai version %s; %s

Usage: kekkai <command> [options]

Commands:
  generate    Generate a manifest of file hashes
  verify      Verify files against a manifest
  version     Show version information
  help        Show this help message

Run 'kekkai <command> -h' for more information on a command.

Examples:
  # Generate manifest
  kekkai generate --target /app --output manifest.json

  # Verify files
  kekkai verify --manifest manifest.json --target /app

`, c.appVersion, runtime.Version())
}

func (c *CLI) printGenerateHelp(flags *flag.FlagSet) {
	fmt.Fprintf(c.errStream, `kekkai generate - Generate a manifest of file hashes

Usage: kekkai generate [options]

Options:
`)
	flags.PrintDefaults()
	fmt.Fprintf(c.errStream, `
Examples:
  # Generate manifest for current directory
  kekkai generate --output manifest.json

  # Generate with specific excludes
  kekkai generate \
    --target /var/www/app \
    --exclude "*.log" \
    --exclude "cache/**" \
    --output manifest.json

  # Generate and upload to S3
  kekkai generate \
    --target /app \
    --s3-bucket my-manifests \
    --app-name myapp
`)
}

func (c *CLI) printVerifyHelp(flags *flag.FlagSet) {
	fmt.Fprintf(c.errStream, `kekkai verify - Verify files against a manifest

Usage: kekkai verify [options]

Options:
`)
	flags.PrintDefaults()
	fmt.Fprintf(c.errStream, `
Examples:
  # Verify from local manifest
  kekkai verify \
    --manifest manifest.json \
    --target /app

  # Verify from S3
  kekkai verify \
    --s3-bucket my-manifests \
    --app-name myapp \
    --target /app

  # Output as JSON
  kekkai verify \
    --manifest manifest.json \
    --target /app \
    --format json
`)
}
