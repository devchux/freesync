package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/devchux/freesync/internal/storage"
	"golang.org/x/crypto/ssh"
)

const (
	StorageS3         = "s3"
	StorageAzureBlob  = "azure-blob"
	StorageSFTP       = "sftp"
	StorageCloudinary = "cloudinary"
)

var (
	ErrObjectExists   = errors.New("storage object already exists")
	ErrObjectNotFound = errors.New("storage object not found")
)

type StorageConfig struct {
	Provider  string
	S3        S3StorageConfig
	AzureBlob AzureBlobStorageConfig
	SFTP      SFTPStorageConfig
}

type S3StorageConfig struct {
	Bucket string
}

type AzureBlobStorageConfig struct {
	ServiceURL string
	Container  string
}

type SFTPStorageConfig struct {
	Address        string
	User           string
	Root           string
	KnownHostsFile string
	Signer         ssh.Signer
}

type storageBackend interface {
	Put(context.Context, string, io.ReadSeeker) error
	List(context.Context, string) ([]storage.Object, error)
	Delete(context.Context, string) error
}

// Storage manages objects through one configured provider.
type Storage struct {
	backend storageBackend
	mu      sync.Mutex
}

// NewStorage initializes the chosen provider. Cloudinary reads CLOUDINARY_URL.
func NewStorage(ctx context.Context, cfg StorageConfig) (*Storage, error) {
	var backend storageBackend
	var err error
	switch cfg.Provider {
	case StorageS3:
		backend, err = storage.NewS3(ctx, cfg.S3.Bucket)
	case StorageAzureBlob:
		backend, err = storage.NewAzureBlob(cfg.AzureBlob.ServiceURL, cfg.AzureBlob.Container)
	case StorageSFTP:
		sftp := cfg.SFTP
		backend, err = storage.NewSFTP(sftp.Address, sftp.User, sftp.Root, sftp.KnownHostsFile, sftp.Signer)
	case StorageCloudinary:
		backend, err = storage.NewCloudinary()
	default:
		return nil, fmt.Errorf("unsupported storage provider %q", cfg.Provider)
	}
	if err != nil {
		return nil, fmt.Errorf("initialize %s storage: %w", cfg.Provider, err)
	}
	return &Storage{backend: backend}, nil
}

// New creates an object and returns ErrObjectExists if its key is already present.
func (s *Storage) New(ctx context.Context, key string, body io.ReadSeeker) error {
	return s.write(ctx, key, body, false)
}

// Update replaces an object and returns ErrObjectNotFound if its key is absent.
func (s *Storage) Update(ctx context.Context, key string, body io.ReadSeeker) error {
	return s.write(ctx, key, body, true)
}

// The existence check and write are serialized within this service instance.
// Other instances and direct provider clients can modify objects between them.
func (s *Storage) write(ctx context.Context, key string, body io.ReadSeeker, replace bool) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("storage key is required")
	}
	if body == nil {
		return errors.New("storage object body is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	objects, err := s.backend.List(ctx, key)
	if err != nil {
		return fmt.Errorf("check storage object %q: %w", key, err)
	}
	exists := false
	for _, object := range objects {
		if object.Key == key {
			exists = true
			break
		}
	}
	switch {
	case exists && !replace:
		return fmt.Errorf("%w: %q", ErrObjectExists, key)
	case !exists && replace:
		return fmt.Errorf("%w: %q", ErrObjectNotFound, key)
	default:
		return s.backend.Put(ctx, key, body)
	}
}

// List returns objects whose keys start with prefix.
func (s *Storage) List(ctx context.Context, prefix string) ([]storage.Object, error) {
	return s.backend.List(ctx, prefix)
}

// Delete removes an object. An absent object is treated as already deleted.
func (s *Storage) Delete(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("storage key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	return s.backend.Delete(ctx, key)
}
