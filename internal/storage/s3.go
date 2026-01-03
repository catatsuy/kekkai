package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/catatsuy/kekkai/internal/manifest"
)

// S3Storage handles S3 operations for manifests
type S3Storage struct {
	client *s3.Client
	bucket string
}

// NewS3Storage creates a new S3 storage client
// Uses EC2 IAM role for authentication
func NewS3Storage(ctx context.Context, bucket string, region string) (*S3Storage, error) {
	if bucket == "" {
		return nil, fmt.Errorf("bucket name is required")
	}

	if region == "" {
		region = "ap-northeast-1" // Default region
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &S3Storage{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
	}, nil
}

// upload uploads a manifest to S3 (internal use only)
func (s *S3Storage) upload(ctx context.Context, key string, m *manifest.Manifest) error {
	// Marshal manifest to JSON
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// Upload to S3
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:               aws.String(s.bucket),
		Key:                  aws.String(key),
		Body:                 bytes.NewReader(data),
		ContentType:          aws.String("application/json"),
		ServerSideEncryption: types.ServerSideEncryptionAes256,
		Metadata: map[string]string{
			"generated-at": m.GeneratedAt,
			"file-count":   fmt.Sprintf("%d", m.FileCount),
		},
	})

	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// download downloads a manifest from S3 (internal use only)
func (s *S3Storage) download(ctx context.Context, key string) (*manifest.Manifest, error) {
	// Get object from S3
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}
	defer result.Body.Close()

	// Read and unmarshal
	var m manifest.Manifest
	decoder := json.NewDecoder(result.Body)
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	return &m, nil
}

// UploadWithVersioning uploads a manifest to a single fixed location
// This is optimized for organizations that deploy frequently throughout the day
// S3 bucket versioning should be enabled to maintain history
func (s *S3Storage) UploadWithVersioning(ctx context.Context, basePath string, appName string, m *manifest.Manifest) (string, error) {
	// Use a fixed key path for single file storage
	key := fmt.Sprintf("%s/%s/manifest.json", basePath, appName)

	// Upload manifest
	if err := s.upload(ctx, key, m); err != nil {
		return "", err
	}

	return key, nil
}

// DownloadManifest downloads the manifest for an app
func (s *S3Storage) DownloadManifest(ctx context.Context, basePath string, appName string) (*manifest.Manifest, error) {
	// Download from the single manifest file
	key := fmt.Sprintf("%s/%s/manifest.json", basePath, appName)
	return s.download(ctx, key)
}
