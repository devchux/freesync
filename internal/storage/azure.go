package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

// AzureBlob stores block blobs in an existing container.
type AzureBlob struct {
	client    *azblob.Client
	container string
}

// NewAzureBlob uses the default Azure credential chain for the account's HTTPS service URL.
func NewAzureBlob(serviceURL, container string) (*AzureBlob, error) {
	if strings.TrimSpace(container) == "" {
		return nil, errors.New("Azure Blob container is required")
	}
	endpoint, err := url.Parse(serviceURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("Azure Blob service URL must be an HTTPS endpoint without credentials, query parameters, or a fragment")
	}

	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("load Azure credentials: %w", err)
	}
	client, err := azblob.NewClient(serviceURL, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("create Azure Blob client: %w", err)
	}
	return &AzureBlob{client: client, container: container}, nil
}

// Put uploads a block blob, replacing an existing blob at the same key.
func (a *AzureBlob) Put(ctx context.Context, key string, body io.ReadSeeker) error {
	return a.put(ctx, key, body, nil)
}

// PutIfUnchanged creates a blob for an empty revision or replaces the matching revision.
func (a *AzureBlob) PutIfUnchanged(ctx context.Context, key, revision, commitID string, body io.ReadSeeker) error {
	conditions := &blob.ModifiedAccessConditions{}
	if revision == "" {
		anyRevision := azcore.ETag("*")
		conditions.IfNoneMatch = &anyRevision
	} else {
		expected := azcore.ETag(revision)
		conditions.IfMatch = &expected
	}
	opts := &azblob.UploadStreamOptions{
		AccessConditions: &blob.AccessConditions{ModifiedAccessConditions: conditions},
	}
	if commitID != "" {
		opts.Metadata = map[string]*string{commitMetadata: &commitID}
	}
	err := a.put(ctx, key, body, opts)
	if bloberror.HasCode(err, bloberror.ConditionNotMet, bloberror.BlobAlreadyExists) ||
		(revision != "" && bloberror.HasCode(err, bloberror.BlobNotFound)) {
		return errors.Join(ErrFileChanged, err)
	}
	return err
}

func (a *AzureBlob) put(ctx context.Context, key string, body io.ReadSeeker, opts *azblob.UploadStreamOptions) error {
	if _, err := a.client.UploadStream(ctx, a.container, key, body, opts); err != nil {
		return fmt.Errorf("put Azure blob %q: %w", key, err)
	}
	return nil
}

// Get streams a blob. The caller must close the returned body.
func (a *AzureBlob) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := a.client.DownloadStream(ctx, a.container, key, nil)
	if err != nil {
		return nil, fmt.Errorf("get Azure blob %q: %w", key, err)
	}
	return out.NewRetryReader(ctx, nil), nil
}

// List returns all blobs whose keys start with prefix.
func (a *AzureBlob) List(ctx context.Context, prefix string) ([]Object, error) {
	pages := a.client.NewListBlobsFlatPager(a.container, &azblob.ListBlobsFlatOptions{Prefix: &prefix})
	objects := make([]Object, 0)
	for pages.More() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list Azure blobs with prefix %q: %w", prefix, err)
		}
		if page.Segment == nil {
			continue
		}
		for _, blob := range page.Segment.BlobItems {
			if blob == nil || blob.Name == nil {
				return nil, errors.New("Azure Blob listing returned a blob without a name")
			}
			object := Object{Key: *blob.Name}
			if blob.Properties != nil {
				if blob.Properties.ContentLength != nil {
					object.Size = *blob.Properties.ContentLength
				}
				if blob.Properties.LastModified != nil {
					object.LastModified = *blob.Properties.LastModified
				}
			}
			objects = append(objects, object)
		}
	}
	return objects, nil
}

// Delete removes the blob at key.
func (a *AzureBlob) Delete(ctx context.Context, key string) error {
	return a.delete(ctx, key, nil)
}

func (a *AzureBlob) DeleteIfUnchanged(ctx context.Context, key, revision string) error {
	tag := azcore.ETag(revision)
	return a.delete(ctx, key, &azblob.DeleteBlobOptions{
		AccessConditions: &blob.AccessConditions{ModifiedAccessConditions: &blob.ModifiedAccessConditions{IfMatch: &tag}},
	})
}

func (a *AzureBlob) delete(ctx context.Context, key string, opts *azblob.DeleteBlobOptions) error {
	_, err := a.client.DeleteBlob(ctx, a.container, key, opts)
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return nil
	}
	if opts != nil && bloberror.HasCode(err, bloberror.ConditionNotMet) {
		return errors.Join(ErrFileChanged, err)
	}
	if err != nil {
		return fmt.Errorf("delete Azure blob %q: %w", key, err)
	}
	return nil
}
