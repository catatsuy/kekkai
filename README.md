# Kekkai

A simple and fast Go tool for file integrity monitoring. Detects unauthorized file modifications caused by OS command injection and other attacks by recording file hashes during deployment and verifying them periodically.

The name "Kekkai" comes from the Japanese word 結界 (kekkai), meaning "barrier" - a protective boundary that keeps unwanted things out, perfectly representing this tool's purpose of protecting your files from tampering.

## Design Philosophy

Kekkai was designed to solve specific challenges in production server environments:

### Why Kekkai?

Traditional tools like `tar` or file sync utilities (e.g., `rsync`) include metadata like timestamps in their comparisons, causing false positives when only timestamps change. In environments with heavy NFS usage or dynamic log directories, existing tools become difficult to configure and maintain.

### Core Principles

1. **Content-Only Hashing**
   - Hashes only file contents, ignoring timestamps and metadata
   - Detects actual content changes, not superficial modifications

2. **Immutable Exclude Rules**
   - Exclude patterns are set during manifest generation only
   - Cannot be modified during verification, preventing attackers from hiding changes
   - Only exclude server-generated files (logs, cache, uploads, NFS mounts)
   - Application dependencies (vendor, node_modules) are monitored as they're part of the deployment

3. **Symlink Security**
   - Uses `os.Lstat` to properly detect symlinks without following them
   - Tracks symbolic links with their target paths (via `os.Readlink`)
   - Hashes the symlink target path itself, not the target's content
   - Detects when symlinks are modified to point to different targets
   - Detects when regular files are replaced with symlinks (and vice versa)
   - Prevents attackers from hiding malicious changes through symlink manipulation

4. **Secure Hash Storage with S3**
   - Deploy servers have write-only access
   - Application servers have read-only access
   - Even if compromised, attackers cannot modify stored hashes
   - Local file output available for testing

5. **Tamper-Resistant Distribution**
   - Single Go binary with minimal dependencies
   - Recommended to run with restricted permissions
   - Configuration should be read from S3 or managed paths, not local files

## Features

- 🚀 **Fast**: Efficient hash calculation with parallel processing
- 🔒 **Secure**: Tamper-proof storage with S3 integration
- 📊 **Monitoring Ready**: Multiple output formats for various monitoring systems
- 🎯 **Deterministic**: Same file structure always produces the same hash
- ☁️ **EC2 Ready**: Authentication via IAM roles

## Installation

```bash
# Build from source
git clone https://github.com/catatsuy/kekkai.git
cd kekkai
make

# Or directly with go build
go build -o ./bin/kekkai ./cmd/kekkai

# Run tests
make test
```

## Usage

### Basic Usage

```bash
# Generate manifest
kekkai generate --target /var/www/app --output manifest.json

# Verify files
kekkai verify --manifest manifest.json --target /var/www/app
```

### Advanced Usage

#### Target Specific Files

```bash
kekkai generate \
  --target /var/www/app \
  --exclude "*.log" \
  --exclude "cache/**" \
  --output manifest.json
```

#### Using S3 Storage

Kekkai stores manifests in S3 for secure, centralized management. Each deployment updates the same `manifest.json` file.

```bash
# For production deployment (must explicitly specify --base-path)
kekkai generate \
  --target /var/www/app \
  --s3-bucket my-manifests \
  --app-name myapp \
  --base-path production  # Explicitly required for production

# For staging/development (uses default "development" if not specified)
kekkai generate \
  --target /var/www/app \
  --s3-bucket my-manifests \
  --app-name myapp \
  --base-path staging

# During verification (must match the base-path used during generation)
kekkai verify \
  --s3-bucket my-manifests \
  --app-name myapp \
  --base-path production \
  --target /var/www/app
```

**Benefits:**
- **Lower S3 costs** - Minimal S3 operations
- **Clean structure** - One manifest file per application

#### Monitoring Integration

```bash
# Add to crontab for periodic checks
*/5 * * * * kekkai verify \
  --s3-bucket my-manifests \
  --app-name myapp \
  --base-path production \
  --target /var/www/app

# Use cache for faster verification (cache in temp directory)
*/5 * * * * kekkai verify \
  --s3-bucket my-manifests \
  --app-name myapp \
  --base-path production \
  --target /var/www/app \
  --use-cache \
  --verify-probability 0.1

# Use persistent cache in custom directory
*/5 * * * * kekkai verify \
  --s3-bucket my-manifests \
  --app-name myapp \
  --base-path production \
  --target /var/www/app \
  --use-cache \
  --cache-dir /var/cache/kekkai \
  --verify-probability 0.1
```

Configure your monitoring system to alert based on your requirements (e.g., alert after consecutive failures).

## Preset Examples

These examples show common exclude patterns for various frameworks. **Important**: Only exclude files generated on the server (logs, cache, uploads). Application dependencies like `vendor` or `node_modules` MUST be monitored as they are part of the deployed application.

For production use, replace `--output manifest.json` with S3 storage options (`--s3-bucket`, `--app-name`, `--base-path`).

### Laravel

```bash
kekkai generate \
  --target /var/www/app \
  --exclude "storage/**" \
  --exclude "bootstrap/cache/**" \
  --exclude "*.log" \
  --output manifest.json
```

### Node.js

```bash
kekkai generate \
  --target /var/www/app \
  --exclude "*.log" \
  --exclude ".npm/**" \
  --exclude "tmp/**" \
  --output manifest.json
```

### Rails

```bash
kekkai generate \
  --target /var/www/app \
  --exclude "log/**" \
  --exclude "tmp/**" \
  --exclude "public/assets/**" \
  --output manifest.json
```

### Python/Django

```bash
kekkai generate \
  --target /var/www/app \
  --exclude "**/__pycache__/**" \
  --exclude "media/**" \
  --exclude "staticfiles/**" \
  --exclude "*.pyc" \
  --output manifest.json
```

## S3 Configuration

### IAM Policies

For deployment server (write access):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject"
      ],
      "Resource": "arn:aws:s3:::my-manifests/*"
    }
  ]
}
```

For production server (read-only):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject"
      ],
      "Resource": "arn:aws:s3:::my-manifests/*"
    }
  ]
}
```

### S3 Bucket Setup

**Recommended:** Enable S3 versioning to maintain history of manifest changes.

```bash
# Optional: Enable versioning for history tracking
aws s3api put-bucket-versioning \
  --bucket my-manifests \
  --versioning-configuration Status=Enabled

# Enable encryption
aws s3api put-bucket-encryption \
  --bucket my-manifests \
  --server-side-encryption-configuration '{
    "Rules": [{
      "ApplyServerSideEncryptionByDefault": {
        "SSEAlgorithm": "AES256"
      }
    }]
  }'

# Optional: Set lifecycle policy to delete old versions after N days
aws s3api put-bucket-lifecycle-configuration \
  --bucket my-manifests \
  --lifecycle-configuration '{
    "Rules": [{
      "Id": "DeleteOldVersions",
      "Status": "Enabled",
      "NoncurrentVersionExpiration": {
        "NoncurrentDays": 30
      }
    }]
  }'
```

## Deployment Flow Example

```bash
#!/bin/bash
# deploy.sh

set -e

APP_NAME="myapp"
DEPLOY_DIR="/var/www/app"
S3_BUCKET="my-manifests"

# 1. Install dependencies locally
cd ./src
composer install --no-dev

# 2. Deploy application to server
rsync -av ./src/ ${DEPLOY_DIR}/

# 3. Generate manifest and save to S3
# Note: For production, explicitly specify --base-path production
kekkai generate \
  --target ${DEPLOY_DIR} \
  --exclude "storage/**" \
  --exclude "bootstrap/cache/**" \
  --s3-bucket ${S3_BUCKET} \
  --app-name ${APP_NAME} \
  --base-path production  # MUST be explicit for production

echo "Deploy completed with integrity manifest"
echo "Manifest saved to: ${S3_BUCKET}/production/${APP_NAME}/manifest.json"
```

## Command Reference

### generate

Generate a manifest file.

```
Options:
  -target string      Target directory (default ".")
  -output string      Output file, "-" for stdout (default "-")
  -exclude string     Exclude pattern (can be specified multiple times)
  -s3-bucket string   S3 bucket name
  -s3-region string   AWS region
  -base-path string   S3 base path (default "development")
  -app-name string    Application name (creates path: {base-path}/{app-name}/manifest.json)
  -format string      Output format: text, json (default "text")
  -workers int        Number of worker threads (0 = auto detect)
  -rate-limit int     Rate limit in bytes per second (0 = no limit)
  -timeout int        Timeout in seconds (default: 300)
```

### verify

Verify file integrity.

```
Options:
  -manifest string    Manifest file path
  -s3-bucket string   S3 bucket name
  -s3-region string   AWS region
  -base-path string   S3 base path (default "development")
  -app-name string    Application name (reads from: {base-path}/{app-name}/manifest.json)
  -target string      Target directory to verify (default ".")
  -format string      Output format: text, json (default "text")
  -workers int              Number of worker threads (0 = auto detect)
  -rate-limit int           Rate limit in bytes per second (0 = no limit)
  -timeout int              Timeout in seconds (default: 300)
  -use-cache                Enable local cache for verification (checks size, mtime, ctime)
  -cache-dir string         Directory for cache file (default: system temp directory)
  -verify-probability float Probability of hash verification with cache hit (0.0-1.0, default: 0.1)
```

## Output Formats

### Text Format (default)

```
✓ Integrity check passed
  Verified 1523 files
```

### JSON Format

```json
{
  "success": true,
  "timestamp": "2024-01-01T00:00:00Z",
  "message": "All files verified successfully",
  "details": {
    "total_files": 1523,
    "verified_files": 1523
  }
}
```

## Glob Pattern Handling

Kekkai uses glob patterns for the `--exclude` option to skip specific files and directories during manifest generation.

### Supported Patterns

| Pattern | Description | Example |
|---------|-------------|---------|
| `*.ext` | Match files with specific extension | `*.log` matches `app.log`, `error.log` |
| `dir/*` | Match all files in a directory | `logs/*` matches `logs/app.log` |
| `dir/**` | Match all files recursively | `cache/**` matches `cache/data.db`, `cache/sessions/abc.txt` |
| `**/*.ext` | Match extension at any depth | `**/*.pyc` matches `app.pyc`, `lib/utils.pyc` |
| `**/dir/*` | Match directory at any depth | `**/logs/*` matches `logs/app.log`, `app/logs/error.log` |
| `path/to/file` | Exact path match | `config/local.ini` matches only that file |

### Pattern Matching Rules

1. **Relative Paths**: All patterns match against relative paths from the target directory
2. **Forward Slashes**: Always use `/` as path separator (even on Windows)
3. **No Negation**: Patterns cannot be negated (no `!pattern` support)
4. **Order Independent**: All patterns are evaluated, order doesn't matter
5. **Immutable**: Exclude patterns cannot be changed during verification

### Common Examples

```bash
# Laravel/Symfony
--exclude "storage/**"          # User uploads
--exclude "var/cache/**"        # Framework cache
--exclude "var/log/**"          # Application logs
--exclude "public/uploads/**"   # Uploaded files

# Python/Django
--exclude "**/__pycache__/**"   # Python cache
--exclude "**/*.pyc"            # Compiled Python
--exclude "media/**"            # User uploads
--exclude "staticfiles/**"      # Collected static files

# Node.js
--exclude "*.log"               # Log files
--exclude "tmp/**"              # Temporary files
--exclude ".npm/**"             # NPM cache

# General
--exclude "*.tmp"               # Temporary files
--exclude "*.bak"               # Backup files
--exclude ".git/**"             # Git repository (if needed)
```

### Important Notes

⚠️ **Do NOT exclude application dependencies**:
- ❌ `--exclude "vendor/**"` (PHP dependencies)
- ❌ `--exclude "node_modules/**"` (Node.js dependencies)
- ❌ `--exclude "venv/**"` (Python virtual environment)

These are part of your deployed application and must be monitored for tampering.

✅ **Only exclude server-generated content**:
- Log files
- Cache directories
- User uploads
- Temporary files
- NFS mounts

### Pattern Evaluation

Patterns are evaluated in this order:
1. Check for `**` recursive matching
2. Special case: `**/*` or `**` matches everything
3. Suffix pattern: `dir/**` matches everything under `dir/`
4. Prefix pattern: `**/*.ext` matches files with extension at any depth
5. Simple glob: Standard shell glob matching with `*` and `?`

## Symlink Handling

Kekkai has comprehensive symlink security to prevent attackers from hiding malicious changes:

### Target Directory Behavior

Kekkai handles symlinks differently depending on where they appear:

#### When Target Itself Is a Symlink

If `--target` points to a symlink (e.g., `/current` → `/releases/20240101`):
- **Automatically resolved**: Uses `filepath.EvalSymlinks` to follow the symlink
- **Operates on real path**: All operations happen in the resolved directory
- **Transparent to user**: Works exactly as if you specified the real directory

Example:
```bash
# These produce identical results:
kekkai generate --target /var/www/current        # Symlink to /var/www/releases/20240101
kekkai generate --target /var/www/releases/20240101  # Direct path
```

#### Symlinks Inside Target Directory

For symlinks found within the target directory:
- **Not followed**: Uses `os.Lstat` to detect them without following
- **Tracked as symlinks**: Stored with `IsSymlink: true` flag
- **Target recorded**: Link target path saved for verification
- **Hash of target path**: Creates hash from `"symlink:" + target_path` string

### How Symlinks Are Processed

1. **Detection**: Uses `os.Lstat` to identify symlinks without following them
2. **Target Tracking**: Reads the link target with `os.Readlink`
3. **Hash Calculation**: Creates hash from `"symlink:" + target_path` string
4. **Verification**: Checks both link type and target path during verification

### What Is Detected

- ✅ Symlink target changes (e.g., `/usr/bin/php` → `/tmp/malicious`)
- ✅ File type changes (regular file → symlink or symlink → regular file)
- ✅ Broken symlinks (target doesn't exist)
- ✅ New symlinks added to the directory
- ✅ Deleted symlinks

### Example

```bash
# Original deployment
/app/config.php -> /etc/app/config.php  # Hash: abc123...

# These changes will be detected:
/app/config.php -> /tmp/fake-config.php  # Modified: different target
/app/config.php (regular file)           # Modified: type change
/app/config.php (deleted)                # Deleted: symlink removed
```

### Security Implications

- Symlinks are never followed during hash calculation
- Only the link itself is hashed, not the target's content
- Prevents directory traversal attacks via symlink manipulation
- Cache system skips symlinks (only caches regular files)

## Troubleshooting

### Q: Hash values change for the same files

A: Kekkai only hashes file contents, so timestamp or permission changes don't affect hashes. Check for line ending differences (CRLF/LF).

### Q: S3 access fails

A: Verify that the EC2 instance has the correct IAM role configured. Also check that the region is specified correctly.

### Q: Verification takes too long

A: For large file sets, use `--exclude` options to skip server-generated directories like logs, cache, and temporary files. You can also optimize performance with:
- `--use-cache`: Enable local cache that checks file metadata (size, mtime, ctime) to skip hash calculation
- `--verify-probability N`: Set probability of hash verification even with cache hit (0.0-1.0, default: 0.1)
- `--workers N`: Adjust the number of worker threads for your system
- `--rate-limit N`: Limit I/O throughput (bytes per second) to reduce system load

**Cache Mode:** When using `--use-cache`, kekkai maintains a local cache file (`.kekkai-cache-{base-name}-{app-name}.json`) in the cache directory (defaults to system temp directory, or specify with `--cache-dir`). Cache files are temporary by nature and will be recreated if missing. It checks file metadata including:
- File size
- Modification time (mtime)
- Change time (ctime) - cannot be easily forged

If all metadata matches, it uses the cached hash. The `--verify-probability` option adds probabilistic verification:
- `0.0`: Always trust cache (fastest, least secure)
- `0.1`: 10% chance to verify hash even with cache hit (default, good balance)
- `1.0`: Always verify hash (most secure, no performance benefit)

The cache file itself is protected with a hash to detect tampering.

⚠️ **Security Note:** Cache mode is secure against casual tampering due to ctime checking, but a sophisticated attacker with root access could potentially forge metadata. The probabilistic verification adds an additional layer of security.

💡 **Cache Behavior:** By default, cache files are stored in the system temp directory (e.g., `/tmp` on Linux/macOS) and may be automatically cleaned by the system. This is intentional - the cache is designed to be ephemeral and will be recreated as needed for performance optimization.

Note that application dependencies (vendor, node_modules) should still be verified as they are part of the deployed application.

### Q: System load is too high during verification

A: Use `--rate-limit` to throttle I/O bandwidth. For example, `--rate-limit 10485760` limits to 10MB/s. This global rate limit is shared across all worker threads, preventing system overload while still allowing parallel processing.

Alternatively, you can use systemd to control resource usage at the OS level:

```bash
# Run with limited CPU and I/O priority (with cache support)
systemd-run --quiet --wait --collect \
  -p Type=oneshot \
  -p CPUQuota=25% -p CPUWeight=100 \
  -p PrivateTmp=no \
  /bin/bash -lc 'nice -n 10 ionice -c2 -n7 /usr/local/bin/kekkai verify \
    --s3-bucket my-manifests \
    --app-name myapp \
    --target /srv/app \
    --use-cache \
    --verify-probability 0.1 \
    --rate-limit 10485760'
```

This approach provides more comprehensive resource control:
- `CPUQuota=25%`: Limits CPU usage to 25%
- `CPUWeight=100`: Sets CPU scheduling weight (lower priority)
- `PrivateTmp=no`: Allows cache persistence in `/tmp` across runs
- `nice -n 10`: Lower process priority
- `ionice -c2 -n7`: Best-effort I/O scheduling with lowest priority
- `--use-cache`: Enables cache for faster verification
- `--verify-probability 0.1`: 10% chance to verify hash even with cache hit
- `--rate-limit 10485760`: Limits I/O to 10MB/s

**Important**: The `PrivateTmp=no` setting is required when using `--use-cache` to ensure cache files persist between systemd-run executions. Without this, systemd creates an isolated `/tmp` directory for each run, preventing cache reuse. If you prefer stronger isolation, use `--cache-dir` to specify a persistent directory outside of `/tmp`:

```bash
# Alternative: Keep PrivateTmp=yes but use a custom cache directory
systemd-run --quiet --wait --collect \
  -p Type=oneshot \
  -p CPUQuota=25% -p CPUWeight=100 \
  -p PrivateTmp=yes \
  /bin/bash -lc 'nice -n 10 ionice -c2 -n7 /usr/local/bin/kekkai verify \
    --s3-bucket my-manifests \
    --app-name myapp \
    --target /srv/app \
    --use-cache \
    --cache-dir /var/cache/kekkai \
    --verify-probability 0.1 \
    --rate-limit 10485760'
```

**Note**: With Go 1.25+, `CPUQuota` also automatically adjusts `GOMAXPROCS` to match the quota, so kekkai will use fewer worker threads when CPU is limited, providing better resource utilization.
