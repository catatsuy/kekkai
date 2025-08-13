package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

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

// Upload uploads a manifest to S3
func (s *S3Storage) Upload(key string, m *manifest.Manifest) error {
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

// Download downloads a manifest from S3
func (s *S3Storage) Download(key string) (*manifest.Manifest, error) {
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
	if err := s.Upload(key, m); err != nil {
		return "", err
	}

	return key, nil
}

// DownloadLatest downloads the latest manifest for an app
func (s *S3Storage) DownloadLatest(basePath string, appName string) (*manifest.Manifest, error) {
	// Direct download from the single manifest file
	key := fmt.Sprintf("%s/%s/manifest.json", basePath, appName)
	return s.Download(key)
}

// List lists all versions of the manifest (requires S3 versioning enabled)
func (s *S3Storage) List(basePath string, appName string) ([]string, error) {
	key := fmt.Sprintf("%s/%s/manifest.json", basePath, appName)

	// List object versions
	result, err := s.client.ListObjectVersions(&s3.ListObjectVersionsInput{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list object versions: %w", err)
	}

	var versions []string
	for _, version := range result.Versions {
		if aws.BoolValue(version.IsLatest) {
			versions = append(versions, fmt.Sprintf("%s (latest)", aws.StringValue(version.VersionId)))
		} else {
			versions = append(versions, aws.StringValue(version.VersionId))
		}
	}

	return versions, nil
}

// Exists checks if a manifest exists in S3
func (s *S3Storage) Exists(key string) (bool, error) {
	_, err := s.client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		// Check if it's a not found error
		if aerr, ok := err.(interface{ Code() string }); ok && aerr.Code() == "NotFound" {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// GetMetadata gets metadata for a manifest
func (s *S3Storage) GetMetadata(key string) (map[string]string, error) {
	result, err := s.client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get object metadata: %w", err)
	}

	metadata := make(map[string]string)
	for k, v := range result.Metadata {
		metadata[k] = aws.StringValue(v)
	}

	return metadata, nil
}

// Reader returns an io.ReadCloser for streaming a manifest
func (s *S3Storage) Reader(key string) (io.ReadCloser, error) {
	result, err := s.client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}

	return result.Body, nil
}
