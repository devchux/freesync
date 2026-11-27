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
	"github.com/aws/smithy-go"
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
	return s.put(ctx, &s3.PutObjectInput{Key: &key, Body: body})
}

// PutIfUnchanged creates an object for an empty revision or replaces the matching revision.
func (s *S3) PutIfUnchanged(ctx context.Context, key, revision, commitID string, body io.ReadSeeker) error {
	in := &s3.PutObjectInput{Key: &key, Body: body}
	if commitID != "" {
		in.Metadata = map[string]string{commitMetadata: commitID}
	}
	if revision == "" {
		in.IfNoneMatch = aws.String("*")
	} else {
		in.IfMatch = &revision
	}
	err := s.put(ctx, in)
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "PreconditionFailed":
			return errors.Join(ErrFileChanged, err)
		case "EntityTooLarge":
			return errors.Join(ErrUploadTooLarge, err)
		case "NoSuchKey", "NotFound":
			if revision != "" {
				return errors.Join(ErrFileChanged, err)
			}
		}
	}
	return err
}

func (s *S3) put(ctx context.Context, in *s3.PutObjectInput) error {
	in.Bucket = &s.bucket
	if _, err := s.client.PutObject(ctx, in); err != nil {
		return fmt.Errorf("put S3 object %q: %w", aws.ToString(in.Key), err)
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
	return s.delete(ctx, key, nil)
}

func (s *S3) DeleteIfUnchanged(ctx context.Context, key, revision string) error {
	return s.delete(ctx, key, &revision)
}

func (s *S3) delete(ctx context.Context, key string, revision *string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key, IfMatch: revision})
	var apiErr smithy.APIError
	if revision != nil && errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return nil
		case "PreconditionFailed":
			return errors.Join(ErrFileChanged, err)
		}
	}
	if err != nil {
		return fmt.Errorf("delete S3 object %q: %w", key, err)
	}
	return nil
}
