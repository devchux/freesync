package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/devchux/freesync/internal/service"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type storageSettings struct {
	service.StorageConfig
	PrivateKeyFile string `json:"privateKeyFile,omitempty"`
}

func runStorage(action string) error {
	if action != "setup" && action != "list" {
		return errors.New(usage)
	}
	path, err := settingsPath()
	if err != nil {
		return err
	}
	path = filepath.Join(filepath.Dir(path), "storage.json")
	var cfg storageSettings
	if action == "setup" {
		cfg, err = promptStorage()
	} else {
		var data []byte
		data, err = os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("run freesync storage setup to connect a provider")
		}
		if err == nil {
			err = json.Unmarshal(data, &cfg)
		}
	}
	if err != nil {
		return err
	}
	if cfg.Provider == service.StorageSFTP {
		key, err := os.ReadFile(cfg.PrivateKeyFile)
		if err != nil {
			return fmt.Errorf("read SSH key: %w", err)
		}
		cfg.SFTP.Signer, err = ssh.ParsePrivateKey(key)
		var encrypted *ssh.PassphraseMissingError
		if errors.As(err, &encrypted) && term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprint(os.Stderr, "SSH key passphrase: ")
			passphrase, readErr := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if readErr != nil {
				return readErr
			}
			cfg.SFTP.Signer, err = ssh.ParsePrivateKeyWithPassphrase(key, passphrase)
			clear(passphrase)
		}
		if err != nil {
			return fmt.Errorf("load SSH key: %w", err)
		}
	}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	store, err := service.NewStorage(ctx, cfg.StorageConfig)
	if err != nil {
		return err
	}
	objects, err := store.List(ctx, "")
	if err != nil {
		return fmt.Errorf("verify storage access: %w", err)
	}
	if action == "list" {
		return json.NewEncoder(os.Stdout).Encode(objects)
	}
	if err := writeSettings(path, cfg); err != nil {
		return fmt.Errorf("save storage settings: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Connected to %s. Found %d objects. Run freesync storage list to view them.\n", cfg.Provider, len(objects))
	return nil
}

func promptStorage() (storageSettings, error) {
	var cfg storageSettings
	reader := bufio.NewScanner(os.Stdin)
	ask := func(label string) string {
		fmt.Fprint(os.Stderr, label)
		if !reader.Scan() {
			return ""
		}
		return strings.TrimSpace(reader.Text())
	}
	cfg.Provider = strings.ToLower(ask("Storage provider (s3, azure-blob, sftp, cloudinary): "))
	switch cfg.Provider {
	case service.StorageS3:
		cfg.S3.Bucket = ask("S3 bucket name (uses your AWS credentials): ")
	case service.StorageAzureBlob:
		cfg.AzureBlob.ServiceURL = ask("Azure storage account URL (uses your Azure credentials): ")
		cfg.AzureBlob.Container = ask("Azure container name: ")
	case service.StorageSFTP:
		cfg.SFTP.Address = ask("SFTP server (host:port): ")
		cfg.SFTP.User = ask("SSH username: ")
		cfg.SFTP.Root = ask("Remote directory: ")
		keyPath := ask("SSH private key file: ")
		hosts := ask("Known hosts file [default ~/.ssh/known_hosts]: ")
		if keyPath == "" {
			return cfg, errors.New("SSH private key file is required")
		}
		if hosts == "" {
			hosts = "~/.ssh/known_hosts"
		}
		var err error
		cfg.PrivateKeyFile, err = localStoragePath(keyPath)
		if err != nil {
			return cfg, err
		}
		cfg.SFTP.KnownHostsFile, err = localStoragePath(hosts)
		if err != nil {
			return cfg, err
		}
	case service.StorageCloudinary:
		cloud := &service.CloudinaryStorageConfig{
			CloudName: ask("Cloudinary cloud name: "),
			APIKey:    ask("Cloudinary API key: "),
		}
		if err := reader.Err(); err != nil {
			return cfg, err
		}
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprint(os.Stderr, "Cloudinary API secret: ")
			secret, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return cfg, err
			}
			cloud.APISecret = strings.TrimSpace(string(secret))
			clear(secret)
		} else {
			cloud.APISecret = ask("Cloudinary API secret: ")
		}
		cfg.Cloudinary = cloud
	default:
		return cfg, fmt.Errorf("unsupported storage provider %q", cfg.Provider)
	}
	return cfg, reader.Err()
}

func localStoragePath(value string) (string, error) {
	if suffix, ok := strings.CutPrefix(value, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, suffix)
	}
	return filepath.Abs(value)
}
