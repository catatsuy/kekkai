# Kekkai - Claude Development Guide

This file contains essential information for Claude instances working on the Kekkai project.

## Project Overview

Kekkai (結界) is a file integrity monitoring tool written in Go that detects unauthorized file modifications on production servers. The name comes from the Japanese word 結界 (kekkai), meaning "barrier" - a protective boundary that keeps unwanted things out.

### Key Features
- Fast parallel hash calculation with worker pools
- Metadata-based caching for performance optimization
- S3 integration for secure manifest storage
- Symlink tracking and security
- Rate limiting and resource control
- Probabilistic verification for balanced security/performance

## Build Commands

```bash
# Build the binary
make                                   # Builds to bin/kekkai
go build -o ./bin/kekkai ./cmd/kekkai  # Alternative

# Run tests
make test                              # Run all tests with coverage
go test ./...                          # Alternative
go test -v ./internal/hash             # Run specific package tests

# Code quality
make vet                               # Run go vet
make staticcheck                       # Run static analysis

# Clean build artifacts
make clean
```

## Project Structure

```
cmd/kekkai/         # Main application entry point
internal/
  ├── cli/          # Command-line interface and argument parsing
  ├── hash/         # Core hash calculation with caching support
  ├── cache/        # Metadata caching system (cache.go, time_*.go)
  ├── manifest/     # Manifest generation and verification
  ├── output/       # Output formatting (text, JSON)
  └── storage/      # S3 storage integration
```

## Critical Coding Guidelines

### Context Handling
- **RULE**: Only use `context.Background()` near main/CLI layer
- Pass context as function parameters, never store in structs
- Use `signal.NotifyContext` for signal handling instead of manual signal channels

### Concurrency Patterns
- **IMPORTANT**: Use Go 1.25+ `wg.Go()` syntax for WaitGroup (DO NOT CHANGE THIS)
- Channel buffer sizes: `min(numWorkers*2, 100)` to balance memory vs performance
- Reuse hash.Hash and buffers within worker goroutines for efficiency

### Variable Naming
- **DO NOT** use short names like `er` for errors
- Use descriptive names: `readErr`, `writeErr`, `calculationErr`

### Error Handling
- Use modern `errors.Is()` and `errors.As()` patterns
- Wrap errors with context using `fmt.Errorf("description: %w", err)`

## Cache System Architecture

The cache system provides performance optimization while maintaining security.

### Cache Files
- **Location**: System temp directory by default (`os.TempDir()`)
- **Filename**: `.kekkai-cache-{base-name}-{app-name}.json` (fixed format)
- **Custom location**: Use `--cache-dir` option

### Cache Content
- **Stores**: file metadata (size, mtime, ctime) + manifest/cache generation times
- **Does NOT store**: hash values (always referenced from manifest)
- **Integrity**: Cache file protected with SHA256 hash

### Metadata Verification Strategy
- **size**: File size in bytes
- **mtime**: Modification time
- **ctime**: Change time (most important - hard to forge)
- **Note**: inode checking removed (ctime is sufficient)

### Platform-Specific Implementation
- **Darwin**: Uses `stat.Ctimespec` for ctime (`internal/cache/time_darwin.go`)
- **Linux**: Uses `stat.Ctim` for ctime (`internal/cache/time_linux.go`)

### Probabilistic Verification
- `--verify-probability 0.1`: 10% chance to verify hash even with cache hit
- `0.0`: Always trust cache (fastest, least secure)
- `1.0`: Always verify hash (most secure, no performance benefit)
- Default: `0.1` (good security/performance balance)

## Security Design

### Cache Security
- Uses ctime (change time) which is difficult to forge without root access
- Cache integrity protected with SHA256 hash to detect tampering
- Probabilistic verification provides additional security layer
- Cache only updated on successful verification

### Deployment Security
- Deploy servers: write-only S3 access
- Application servers: read-only S3 access
- Exclude patterns immutable during verification
- Symlink target tracking prevents manipulation

### systemd Integration
- Use resource limits: `CPUQuota`, `CPUWeight`, `nice`, `ionice`
- **CRITICAL**: `PrivateTmp=yes` isolates `/tmp` - use `--cache-dir` for cache persistence

## Performance Optimization

### Memory Management
- Channel buffers: `min(numWorkers*2, 100)` prevents excessive memory usage
- Reuse hash.Hash and byte buffers within workers
- Pre-allocate slices with known capacity

### I/O Optimization
- Use `--use-cache` for metadata-based fast verification
- Adjust `--workers` based on CPU cores
- Use `--rate-limit` to control I/O throughput
- Balance `--verify-probability` for security vs performance

## Common Issues and Solutions

### Build Issues
- **Linux ctime error**: Fixed with platform-specific time handling implementations
- **Test timing failures**: Use 100ms+ sleep for filesystem timestamp changes

### Production Issues Addressed
- **Memory usage**: Channel buffer optimization prevents OOM with large directories
- **Missing timeouts**: Added configurable timeout support (default 300s)
- **Cache performance**: Comprehensive metadata checking with probabilistic verification

## Testing Guidelines

### Cache Tests
- Require filesystem timestamp changes (use 100ms+ sleep between operations)
- Test concurrent access with multiple goroutines
- Verify cache integrity and tamper detection
- Test platform-specific ctime handling

### Hash Calculation Tests
- Test symlink handling and target changes
- Verify deterministic hash calculation across runs
- Test rate limiting functionality
- Test context cancellation behavior

## Key Implementation Files

### Core Hash Logic
- **internal/hash/hash.go:315-333**: Cache metadata checking logic
- **internal/hash/hash.go:277-421**: Parallel hash calculation with workers

### Cache Implementation
- **internal/cache/cache.go**: Metadata verification and storage
- **internal/cache/time_*.go**: Platform-specific ctime handling

### CLI Integration
- **internal/cli/cli.go:326-338**: Cache mode CLI integration
- **internal/cli/cli.go:147-156**: Signal handling with context

### Manifest Management
- **internal/manifest/manifest.go:148-157**: Cache update on successful verification
- **internal/manifest/manifest.go:188-237**: Core verification logic

## Recent Major Changes

### Removed Features
- Fast-verify mode (replaced with comprehensive cache system)
- Complex security implementation for .kekkaiignore
- "AndContext" API methods (only context-required versions remain)

### Added Features
- Metadata cache system with ctime verification
- Probabilistic hash verification
- Platform-specific ctime handling
- Configurable cache directories
- Enhanced timeout support
- systemd resource control integration

### Performance Improvements
- Optimized channel buffer sizes for memory efficiency
- Hash.Hash and buffer reuse within workers
- Rate limiting with shared limiter across workers

## Development Workflow

1. **Always test on both Darwin and Linux** for platform compatibility
2. **Use cache for performance testing** but verify security implications
3. **Follow context passing patterns** - never store context in structs
4. **Update tests** when modifying cache or hash calculation logic
5. **Consider memory usage** when working with large file sets
6. **Document CLI changes** in README.md

## Architecture Decisions

### Single File S3 Storage
- Uses S3 native versioning instead of separate versioned files
- Reduces S3 operation costs for frequent deployments
- Path: `{base-path}/{app-name}/manifest.json`

### Immutable Exclude Patterns
- Set during manifest generation only
- Cannot be modified during verification
- Prevents attackers from hiding changes

### Content-Only Hashing
- Ignores timestamps and metadata changes
- Detects actual content modifications
- Deterministic across identical file structures

This guide reflects the current state after extensive optimization and security improvements. Always refer to this file when working on Kekkai to maintain consistency with established patterns and security practices.
