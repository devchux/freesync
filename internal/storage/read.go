package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/cloudinary/cloudinary-go/v2/api"
	"github.com/cloudinary/cloudinary-go/v2/api/admin"
)

type FileInfo struct {
	Object
	Revision string
	CommitID string
}

var ErrFileChanged = errors.New("remote file changed while open")
var ErrRangeUnsupported = errors.New("remote endpoint does not support byte ranges")

// Reader retrieves metadata and reads a bounded portion of an open file.
type Reader interface {
	Stat(context.Context, string) (FileInfo, error)
	ReadRange(context.Context, FileInfo, int64, []byte) (int, error)
}

func byteRange(offset int64, size int) string {
	return fmt.Sprintf("bytes=%d-%d", offset, offset+int64(size)-1)
}

func readRangeBody(body io.ReadCloser, contentRange string, file FileInfo, offset int64, buffer []byte) (int, error) {
	defer body.Close()
	if contentRange == "" {
		return 0, ErrRangeUnsupported
	}
	expected := fmt.Sprintf("bytes %d-%d/%d", offset, offset+int64(len(buffer))-1, file.Size)
	if contentRange != expected {
		return 0, fmt.Errorf("unexpected content range for %q: got %q, expected %q", file.Key, contentRange, expected)
	}
	return io.ReadFull(body, buffer)
}

func (s *S3) Stat(ctx context.Context, key string) (FileInfo, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return FileInfo{}, fs.ErrNotExist
		}
		return FileInfo{}, fmt.Errorf("stat S3 object %q: %w", key, err)
	}
	return FileInfo{Object: Object{Key: key, Size: aws.ToInt64(out.ContentLength), LastModified: aws.ToTime(out.LastModified)}, Revision: aws.ToString(out.ETag), CommitID: out.Metadata[commitMetadata]}, nil
}

func (s *S3) ReadRange(ctx context.Context, file FileInfo, offset int64, buffer []byte) (int, error) {
	in := &s3.GetObjectInput{Bucket: &s.bucket, Key: &file.Key, Range: aws.String(byteRange(offset, len(buffer)))}
	if file.Revision != "" {
		in.IfMatch = &file.Revision
	}
	out, err := s.client.GetObject(ctx, in)
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "PreconditionFailed" {
			return 0, ErrFileChanged
		}
		return 0, fmt.Errorf("read S3 object %q: %w", file.Key, err)
	}
	return readRangeBody(out.Body, aws.ToString(out.ContentRange), file, offset, buffer)
}

func (a *AzureBlob) Stat(ctx context.Context, key string) (FileInfo, error) {
	client := a.client.ServiceClient().NewContainerClient(a.container).NewBlobClient(key)
	out, err := client.GetProperties(ctx, nil)
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return FileInfo{}, fs.ErrNotExist
	}
	if err != nil {
		return FileInfo{}, fmt.Errorf("stat Azure blob %q: %w", key, err)
	}
	file := FileInfo{Object: Object{Key: key}}
	if out.ContentLength != nil {
		file.Size = *out.ContentLength
	}
	if out.LastModified != nil {
		file.LastModified = *out.LastModified
	}
	if out.ETag != nil {
		file.Revision = string(*out.ETag)
	}
	for key, value := range out.Metadata {
		if strings.EqualFold(key, commitMetadata) && value != nil {
			file.CommitID = *value
		}
	}
	return file, nil
}

func (a *AzureBlob) ReadRange(ctx context.Context, file FileInfo, offset int64, buffer []byte) (int, error) {
	opts := &blob.DownloadStreamOptions{Range: blob.HTTPRange{Offset: offset, Count: int64(len(buffer))}}
	if file.Revision != "" {
		tag := azcore.ETag(file.Revision)
		opts.AccessConditions = &blob.AccessConditions{ModifiedAccessConditions: &blob.ModifiedAccessConditions{IfMatch: &tag}}
	}
	out, err := a.client.DownloadStream(ctx, a.container, file.Key, opts)
	if bloberror.HasCode(err, bloberror.ConditionNotMet) {
		return 0, ErrFileChanged
	}
	if err != nil {
		return 0, fmt.Errorf("read Azure blob %q: %w", file.Key, err)
	}
	return readRangeBody(out.NewRetryReader(ctx, nil), aws.ToString(out.ContentRange), file, offset, buffer)
}

func (s *SFTP) Stat(ctx context.Context, key string) (FileInfo, error) {
	if !validSFTPKey(key) {
		return FileInfo{}, fs.ErrNotExist
	}
	client, closeConn, err := s.connect(ctx)
	if err != nil {
		return FileInfo{}, err
	}
	defer closeConn()
	info, err := client.Lstat(path.Join(s.root, key))
	if err != nil {
		return FileInfo{}, fmt.Errorf("stat SFTP file %q: %w", key, err)
	}
	if !info.Mode().IsRegular() {
		return FileInfo{}, fs.ErrNotExist
	}
	return FileInfo{Object: Object{Key: key, Size: info.Size(), LastModified: info.ModTime()}}, nil
}

func (s *SFTP) ReadRange(ctx context.Context, file FileInfo, offset int64, buffer []byte) (int, error) {
	remote, err := s.openFile(ctx, file.Key)
	if err != nil {
		return 0, err
	}
	defer remote.Close()
	check := func() error {
		info, err := remote.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != file.Size || !info.ModTime().Equal(file.LastModified) {
			return ErrFileChanged
		}
		return nil
	}
	if err := check(); err != nil {
		return 0, err
	}
	n, err := remote.ReadAt(buffer, offset)
	if err != nil {
		return n, err
	}
	if err := check(); err != nil {
		return 0, err
	}
	return n, nil
}

func (c *Cloudinary) Stat(ctx context.Context, key string) (FileInfo, error) {
	out, err := c.client.Admin.Asset(ctx, admin.AssetParams{PublicID: key, AssetType: api.File, DeliveryType: api.Authenticated})
	if err != nil {
		return FileInfo{}, fmt.Errorf("stat Cloudinary object %q: %w", key, err)
	}
	modified := out.CreatedAt
	if out.Version > 0 {
		modified = time.Unix(int64(out.Version), 0).UTC()
	}
	return FileInfo{Object: Object{Key: key, Size: int64(out.Bytes), LastModified: modified}, Revision: strconv.Itoa(out.Version)}, nil
}

func (c *Cloudinary) ReadRange(ctx context.Context, file FileInfo, offset int64, buffer []byte) (int, error) {
	version, err := strconv.Atoi(file.Revision)
	if err != nil || version <= 0 {
		return 0, errors.New("Cloudinary asset version is required for random access")
	}
	if c.fallback != nil {
		c.fallback.mu.Lock()
		defer c.fallback.mu.Unlock()
		if c.fallback.file != nil && c.fallback.info.Key == file.Key && c.fallback.info.Revision == file.Revision && c.fallback.info.Size == file.Size {
			return c.fallback.file.ReadAt(buffer, offset)
		}
	}
	asset, err := c.client.File(file.Key)
	if err != nil {
		return 0, err
	}
	asset.Version = version
	asset.DeliveryType = api.Authenticated
	asset.Config.URL.Secure = true
	asset.Config.URL.SignURL = true
	signedURL, err := asset.String()
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil || req.URL.Scheme != "https" {
		return 0, errors.New("invalid Cloudinary range download endpoint")
	}
	req.Header.Set("Range", byteRange(offset, len(buffer)))
	req.Header.Set("Accept-Encoding", "identity")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			err = requestErr.Err
		}
		return 0, fmt.Errorf("read Cloudinary object %q: %w", file.Key, err)
	}
	if res.StatusCode == http.StatusOK {
		if c.fallback != nil {
			return c.fallback.readFull(res, file, offset, buffer)
		}
		res.Body.Close()
		return 0, ErrRangeUnsupported
	}
	if res.StatusCode != http.StatusPartialContent {
		res.Body.Close()
		return 0, fmt.Errorf("read Cloudinary object %q: HTTP %d", file.Key, res.StatusCode)
	}
	return readRangeBody(res.Body, res.Header.Get("Content-Range"), file, offset, buffer)
}
