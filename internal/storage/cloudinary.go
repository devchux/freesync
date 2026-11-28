package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api"
	"github.com/cloudinary/cloudinary-go/v2/api/admin"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
)

// Cloudinary stores files as authenticated raw assets, using keys as public IDs.
type Cloudinary struct {
	client   *cloudinary.Cloudinary
	fallback *cloudinaryFallback
}

// NewCloudinary configures access to a Cloudinary product environment.
func NewCloudinary(cloudName, apiKey, apiSecret string) (*Cloudinary, error) {
	if strings.TrimSpace(cloudName) == "" || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(apiSecret) == "" {
		return nil, errors.New("Cloudinary cloud name, API key, and API secret are required")
	}
	client, err := cloudinary.NewFromParams(cloudName, apiKey, apiSecret)
	if err != nil {
		return nil, errors.New("invalid Cloudinary configuration")
	}
	return &Cloudinary{client: client}, nil
}

// Put uploads or replaces a raw asset. Include any file extension in the key.
func (c *Cloudinary) Put(ctx context.Context, key string, body io.ReadSeeker) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("Cloudinary key is required")
	}
	overwrite := true
	_, err := c.client.Upload.Upload(ctx, body, uploader.UploadParams{
		PublicID:     key,
		ResourceType: string(api.File),
		Type:         api.Authenticated,
		Overwrite:    &overwrite,
	})
	if err != nil {
		return fmt.Errorf("put Cloudinary object %q: %w", key, err)
	}
	return nil
}

// Get streams the original asset using a short-lived signed download URL.
// The caller must close the returned body.
func (c *Cloudinary) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("Cloudinary key is required")
	}
	expires := time.Now().Add(5 * time.Minute)
	signedURL, err := c.client.Upload.PrivateDownloadURL(uploader.PrivateDownloadURLParams{
		PublicID:     key,
		ResourceType: api.File,
		DeliveryType: string(api.Authenticated),
		ExpiresAt:    &expires,
	})
	if err != nil {
		return nil, fmt.Errorf("sign download for Cloudinary object %q: %w", key, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil || req.URL.Scheme != "https" || req.URL.Host == "" {
		return nil, errors.New("Cloudinary download requires a valid HTTPS endpoint")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		// The transport error's URL contains the signed download query.
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			err = requestErr.Err
		}
		return nil, fmt.Errorf("get Cloudinary object %q: %w", key, err)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("get Cloudinary object %q: HTTP %d", key, res.StatusCode)
	}
	return res.Body, nil
}

// List returns authenticated raw assets whose public IDs start with prefix.
func (c *Cloudinary) List(ctx context.Context, prefix string) ([]Object, error) {
	params := admin.AssetsParams{
		AssetType:    api.File,
		DeliveryType: string(api.Authenticated),
		Prefix:       prefix,
		MaxResults:   500,
	}
	objects := make([]Object, 0)
	for {
		page, err := c.client.Admin.Assets(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("list Cloudinary objects with prefix %q: %w", prefix, err)
		}
		for _, asset := range page.Assets {
			modified := asset.CreatedAt
			if asset.Version > 0 {
				modified = time.Unix(int64(asset.Version), 0).UTC()
			}
			objects = append(objects, Object{
				Key:          asset.PublicID,
				Size:         int64(asset.Bytes),
				LastModified: modified,
			})
		}
		if page.NextCursor == "" {
			return objects, nil
		}
		params.NextCursor = page.NextCursor
	}
}

// Delete removes the authenticated raw asset at key.
func (c *Cloudinary) Delete(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("Cloudinary key is required")
	}
	result, err := c.client.Upload.Destroy(ctx, uploader.DestroyParams{
		PublicID:     key,
		ResourceType: string(api.File),
		Type:         string(api.Authenticated),
	})
	if err != nil {
		return fmt.Errorf("delete Cloudinary object %q: %w", key, err)
	}
	switch result.Result {
	case "ok", "not found":
		return nil
	default:
		return fmt.Errorf("delete Cloudinary object %q: unexpected result %q", key, result.Result)
	}
}
