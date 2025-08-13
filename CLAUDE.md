# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kekkai (結界) is a file integrity monitoring tool designed to detect unauthorized file modifications on production servers. It records file hashes during deployment and verifies them periodically to detect tampering from OS command injection or other attacks.

## Build and Test Commands

```bash
# Build the binary
make                                   # Builds to bin/kekkai
go build -o ./bin/kekkai ./cmd/kekkai  # Alternative

# Run tests
make test              # Run all tests with coverage
go test ./...          # Alternative
go test -v ./internal/hash  # Run specific package tests

# Code quality
make vet               # Run go vet
make staticcheck       # Run static analysis (requires staticcheck installed)

# Clean build artifacts
make clean
```

## Architecture Overview

### Core Components

1. **CLI Entry Point** (`cmd/kekkai/main.go`)
   - Minimal main function that delegates to CLI handler

2. **CLI Handler** (`internal/cli/cli.go`)
   - Commands: `generate` and `verify`
   - Handles argument parsing, S3 integration, and output formatting
   - Default base-path is "development" (production must be explicit)
   - Removed features: include patterns (only excludes), mackerel format

3. **Hash Calculator** (`internal/hash/hash.go`)
   - Content-only hashing (ignores timestamps/metadata)
   - Parallel processing with worker pool
   - Symlink handling: resolves target directory symlinks, skips file symlinks
   - Pattern matching: only exclude patterns (no include)
   - Uses SHA256 for individual files and deterministic total hash

4. **Manifest Management** (`internal/manifest/manifest.go`)
   - JSON structure with version, total hash, file list, and excludes
   - Immutable exclude rules - set at generation, enforced at verification
   - Integration with hash calculator for generation and verification

5. **S3 Storage** (`internal/storage/s3.go`)
   - Single-file storage mode optimized for frequent deploys
   - Stores at `{base-path}/{app-name}/manifest.json`
   - Relies on S3 versioning for history (no separate versioned files)
   - EC2 IAM role authentication

6. **Output Formatter** (`internal/output/formatter.go`)
   - Formats: text (default), JSON
   - Separate types for generation and verification results

### Key Design Decisions

1. **Single File S3 Storage**: Unlike traditional versioned storage, uses S3's native versioning for history. Reduces S3 operation costs for frequent deployments.

2. **No Include Patterns**: Simplified to "include everything except excludes" pattern. Reduces complexity and confusion about pattern precedence.

3. **Symlink Resolution**: Target directory symlinks are resolved, but symlinks within the tree are skipped for security.

4. **Immutable Excludes**: Exclude patterns stored in manifest cannot be overridden during verification, preventing attackers from hiding changes.

## S3 Configuration Requirements

- Bucket must have versioning enabled for single-file storage mode
- Deploy servers need write access
- Application servers need read-only access
- Default region: ap-northeast-1

## Testing Approach

- Unit tests with test fixtures in `testdata/` directories
- Symlink handling tests require proper test data setup
- Parallel hash calculation tests verify deterministic output
- Manifest verification tests cover modification, deletion, and addition scenarios
