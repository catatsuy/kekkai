package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

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

// UploadWithVersioning uploads a manifest with automatic versioning
func (s *S3Storage) UploadWithVersioning(basePath string, appName string, m *manifest.Manifest) (string, error) {
	// Generate versioned key
	timestamp := time.Now().UTC().Format("20060102-150405")
	key := fmt.Sprintf("%s/%s/%s-%s.json", basePath, appName, timestamp, m.TotalHash[:8])

	// Upload manifest
	if err := s.Upload(key, m); err != nil {
		return "", err
	}

	// Update latest pointer
	latestKey := fmt.Sprintf("%s/%s/latest.json", basePath, appName)
	if err := s.updateLatestPointer(latestKey, key, m); err != nil {
		return "", fmt.Errorf("failed to update latest pointer: %w", err)
	}

	return key, nil
}

// updateLatestPointer updates the latest.json file to point to the current manifest
func (s *S3Storage) updateLatestPointer(latestKey string, currentKey string, m *manifest.Manifest) error {
	metadata := map[string]interface{}{
		"current_key": currentKey,
		"total_hash":  m.TotalHash,
		"updated_at":  time.Now().UTC().Format(time.RFC3339),
		"file_count":  m.FileCount,
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	_, err = s.client.PutObject(&s3.PutObjectInput{
		Bucket:               aws.String(s.bucket),
		Key:                  aws.String(latestKey),
		Body:                 bytes.NewReader(data),
		ContentType:          aws.String("application/json"),
		ServerSideEncryption: aws.String("AES256"),
	})

	return err
}

// DownloadLatest downloads the latest manifest for an app
func (s *S3Storage) DownloadLatest(basePath string, appName string) (*manifest.Manifest, error) {
	// First, get the latest pointer
	latestKey := fmt.Sprintf("%s/%s/latest.json", basePath, appName)

	result, err := s.client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(latestKey),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get latest pointer: %w", err)
	}
	defer result.Body.Close()

	// Read latest metadata
	var metadata map[string]interface{}
	decoder := json.NewDecoder(result.Body)
	if err := decoder.Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to decode latest metadata: %w", err)
	}

	// Get actual manifest key
	currentKey, ok := metadata["current_key"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid latest pointer format")
	}

	// Download actual manifest
	return s.Download(currentKey)
}

// List lists all manifests for an app
func (s *S3Storage) List(basePath string, appName string) ([]string, error) {
	prefix := fmt.Sprintf("%s/%s/", basePath, appName)

	result, err := s.client.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	var keys []string
	for _, obj := range result.Contents {
		key := aws.StringValue(obj.Key)
		// Skip latest.json
		if !bytes.HasSuffix([]byte(key), []byte("latest.json")) {
			keys = append(keys, key)
		}
	}

	return keys, nil
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
