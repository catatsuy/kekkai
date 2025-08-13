package storage

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/catatsuy/kekkai/internal/manifest"
)

// S3Storage handles S3 operations for manifests
type S3Storage struct {
	client *s3.S3
	bucket string
}

// NewS3Storage creates a new S3 storage client
// Uses EC2 IAM role for authentication
func NewS3Storage(bucket string, region string) (*S3Storage, error) {
	if bucket == "" {
		return nil, fmt.Errorf("bucket name is required")
	}

	if region == "" {
		region = "ap-northeast-1" // Default region
	}

	// Create session using EC2 IAM role
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
		// EC2 IAM role credentials are automatically loaded
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return &S3Storage{
		client: s3.New(sess),
		bucket: bucket,
	}, nil
}

// upload uploads a manifest to S3 (internal use only)
func (s *S3Storage) upload(key string, m *manifest.Manifest) error {
	// Marshal manifest to JSON
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// Upload to S3
	_, err = s.client.PutObject(&s3.PutObjectInput{
		Bucket:               aws.String(s.bucket),
		Key:                  aws.String(key),
		Body:                 bytes.NewReader(data),
		ContentType:          aws.String("application/json"),
		ServerSideEncryption: aws.String("AES256"), // Enable server-side encryption
		Metadata: map[string]*string{
			"total-hash":   aws.String(m.TotalHash),
			"generated-at": aws.String(m.GeneratedAt),
			"file-count":   aws.String(fmt.Sprintf("%d", m.FileCount)),
		},
	})

	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// download downloads a manifest from S3 (internal use only)
func (s *S3Storage) download(key string) (*manifest.Manifest, error) {
	// Get object from S3
	result, err := s.client.GetObject(&s3.GetObjectInput{
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
func (s *S3Storage) UploadWithVersioning(basePath string, appName string, m *manifest.Manifest) (string, error) {
	// Use a fixed key path for single file storage
	key := fmt.Sprintf("%s/%s/manifest.json", basePath, appName)

	// Upload manifest
	if err := s.upload(key, m); err != nil {
		return "", err
	}

	return key, nil
}

// DownloadManifest downloads the manifest for an app
func (s *S3Storage) DownloadManifest(basePath string, appName string) (*manifest.Manifest, error) {
	// Download from the single manifest file
	key := fmt.Sprintf("%s/%s/manifest.json", basePath, appName)
	return s.download(key)
}
