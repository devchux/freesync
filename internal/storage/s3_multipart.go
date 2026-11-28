package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const multipartThreshold = 64 << 20
const maxMultipartParts = 10000

func remainingSize(body io.ReadSeeker) (int64, error) {
	start, err := body.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	end, err := body.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	_, err = body.Seek(start, io.SeekStart)
	return end - start, err
}

func (s *S3) putMultipart(ctx context.Context, in *s3.PutObjectInput, body io.ReadSeeker, size int64) (result error) {
	partSize := max(int64(multipartThreshold), 1+(size-1)/maxMultipartParts)
	if partSize > 5<<30 {
		return ErrUploadTooLarge
	}
	upload, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: &s.bucket, Key: in.Key, Metadata: in.Metadata,
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	})
	if err != nil {
		return fmt.Errorf("start S3 multipart upload: %w", err)
	}
	defer func() {
		if result == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, err := s.client.AbortMultipartUpload(cleanup, &s3.AbortMultipartUploadInput{
			Bucket: &s.bucket, Key: in.Key, UploadId: upload.UploadId,
		})
		// Completion might have succeeded despite a lost response. Preserve the
		// original error for the queue's commit-ID check and retry handling.
		if err != nil {
			var missing *types.NoSuchUpload
			if !errors.As(err, &missing) {
				result = errors.Join(result, fmt.Errorf("abort S3 multipart upload: %w", err))
			}
		}
	}()
	parts := make([]types.CompletedPart, 0, 1+(size-1)/partSize)
	for size > 0 {
		length := min(size, partSize)
		part, err := uploadPart(ctx, s, in.Key, upload.UploadId, body, length, int32(len(parts)+1))
		if err != nil {
			return err
		}
		parts = append(parts, part)
		size -= length
	}
	_, err = s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: &s.bucket, Key: in.Key, UploadId: upload.UploadId,
		IfMatch: in.IfMatch, IfNoneMatch: in.IfNoneMatch,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	if err != nil {
		return fmt.Errorf("complete S3 multipart upload: %w", err)
	}
	return nil
}

func uploadPart(ctx context.Context, s *S3, key, uploadID *string, body io.ReadSeeker, length int64, number int32) (types.CompletedPart, error) {
	// Each SDK request needs its own seekable view for request retries.
	seeker := body
	start, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return types.CompletedPart{}, err
	}
	reader := io.NewSectionReader(seekingReaderAt{seeker}, start, length)
	out, err := s.client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: &s.bucket, Key: key, UploadId: uploadID,
		Body: reader, ContentLength: aws.Int64(length), PartNumber: &number,
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	})
	if err != nil {
		return types.CompletedPart{}, fmt.Errorf("upload S3 part %d: %w", number, err)
	}
	if _, err := seeker.Seek(start+length, io.SeekStart); err != nil {
		return types.CompletedPart{}, err
	}
	return types.CompletedPart{PartNumber: &number, ETag: out.ETag, ChecksumSHA256: out.ChecksumSHA256}, nil
}

type seekingReaderAt struct{ io.ReadSeeker }

func (r seekingReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return io.ReadFull(r, p)
}
