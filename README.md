# Kekkai

A simple and fast Go tool for file integrity monitoring. Detects unauthorized file modifications caused by OS command injection and other attacks by recording file hashes during deployment and verifying them periodically.

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
go build -o kekkai ./cmd/kekkai

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
  --include "*.php" \
  --include "vendor/**" \
  --exclude "*.log" \
  --exclude "cache/**" \
  --output manifest.json
```

#### Using S3 Storage (Single File)

For organizations that deploy frequently, Kekkai now uses a single-file storage approach. Each deployment overwrites the same `manifest.json` file, relying on S3's built-in versioning for history.

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

**Benefits of Single File Storage:**
- **Lower S3 costs** - Fewer PUT/GET/LIST operations
- **Cleaner bucket** - Only one manifest file per application
- **Automatic history** - S3 versioning maintains all previous versions

#### Monitoring Integration

```bash
# Add to crontab for periodic checks
*/5 * * * * kekkai verify \
  --s3-bucket my-manifests \
  --app-name myapp \
  --base-path production \
  --target /var/www/app
```

Configure your monitoring system to alert based on your requirements (e.g., alert after consecutive failures).

## Preset Examples

### Laravel

```bash
kekkai generate \
  --target /var/www/app \
  --include "**/*.php" \
  --include "composer.json" \
  --include "composer.lock" \
  --include "vendor/**" \
  --include ".env" \
  --exclude "storage/**" \
  --exclude "bootstrap/cache/**" \
  --output manifest.json
```

### Node.js

```bash
kekkai generate \
  --target /var/www/app \
  --include "**/*.js" \
  --include "**/*.json" \
  --include "node_modules/**" \
  --exclude "node_modules/.cache/**" \
  --exclude "dist/**" \
  --output manifest.json
```

### Rails

```bash
kekkai generate \
  --target /var/www/app \
  --include "**/*.rb" \
  --include "Gemfile*" \
  --include "config/**" \
  --exclude "log/**" \
  --exclude "tmp/**" \
  --output manifest.json
```

### Python/Django

```bash
kekkai generate \
  --target /var/www/app \
  --include "**/*.py" \
  --include "requirements.txt" \
  --include "*/migrations/**" \
  --exclude "**/__pycache__/**" \
  --exclude "media/**" \
  --exclude "staticfiles/**" \
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
        "s3:PutObject",
        "s3:PutObjectAcl"
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
        "s3:GetObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::my-manifests",
        "arn:aws:s3:::my-manifests/*"
      ]
    }
  ]
}
```

### S3 Bucket Setup

**Important:** S3 versioning must be enabled for history tracking with single-file storage.

```bash
# Enable versioning (REQUIRED for single-file storage)
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

# 1. Deploy application
rsync -av ./src/ ${DEPLOY_DIR}/

# 2. Install dependencies
cd ${DEPLOY_DIR}
composer install --no-dev

# 3. Generate manifest and save to S3 (single file)
# Note: For production, explicitly specify --base-path production
kekkai generate \
  --target ${DEPLOY_DIR} \
  --include "**/*.php" \
  --include "vendor/**" \
  --exclude "storage/**" \
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
  -include string     Include pattern (can be specified multiple times)
  -exclude string     Exclude pattern (can be specified multiple times)
  -s3-bucket string   S3 bucket name
  -s3-key string      S3 key path
  -s3-region string   AWS region
  -base-path string   S3 base path (default "development")
  -app-name string    Application name for S3 versioning
  -format string      Output format: text, json (default "text")
```

### verify

Verify file integrity.

```
Options:
  -manifest string    Manifest file path
  -s3-bucket string   S3 bucket name
  -s3-key string      S3 key path
  -s3-region string   AWS region
  -base-path string   S3 base path (default "development")
  -app-name string    Application name
  -target string      Target directory to verify (default ".")
  -format string      Output format: text, json (default "text")
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

## Troubleshooting

### Q: Hash values change for the same files

A: Kekkai only hashes file contents, so timestamp or permission changes don't affect hashes. Check for line ending differences (CRLF/LF).

### Q: S3 access fails

A: Verify that the EC2 instance has the correct IAM role configured. Also check that the region is specified correctly.

### Q: Verification takes too long

A: For large file sets, consider using `--exclude` options to skip unnecessary directories (node_modules, vendor, etc.).
