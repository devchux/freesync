package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3 stores objects in an existing bucket.
type S3 struct {
	client *s3.Client
	bucket string
}

// NewS3 uses the AWS SDK's default credential and region configuration.
func NewS3(ctx context.Context, bucket string) (*S3, error) {
	if strings.TrimSpace(bucket) == "" {
		return nil, errors.New("S3 bucket is required")
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	if cfg.Region == "" {
		return nil, errors.New("AWS region is required")
	}
	return &S3{client: s3.NewFromConfig(cfg), bucket: bucket}, nil
}

// Put uploads an object using a seekable body so the SDK can retry the request.
func (s *S3) Put(ctx context.Context, key string, body io.ReadSeeker) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	if err != nil {
		return fmt.Errorf("put S3 object %q: %w", key, err)
	}
	return nil
}

// Get streams an object. The caller must close the returned body.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get S3 object %q: %w", key, err)
	}
	return out.Body, nil
}

// List returns all objects whose keys start with prefix.
func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	objects := make([]Object, 0)
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list S3 objects with prefix %q: %w", prefix, err)
		}
		for _, object := range page.Contents {
			objects = append(objects, Object{
				Key:          aws.ToString(object.Key),
				Size:         aws.ToInt64(object.Size),
				LastModified: aws.ToTime(object.LastModified),
			})
		}
	}
	return objects, nil
}

// Delete removes the object at key.
func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete S3 object %q: %w", key, err)
	}
	return nil
}
