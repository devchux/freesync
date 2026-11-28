package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/devchux/freesync/internal/cache"
	"github.com/devchux/freesync/internal/database"
	"github.com/devchux/freesync/internal/drive"
	"github.com/devchux/freesync/internal/managed"
	"github.com/devchux/freesync/internal/service"
)

const managedUsage = "Usage: freesync managed [serve | create | invite | accept | revoke | mount | export | backup | restore] [flags]"

func runManaged(args []string) error {
	if len(args) == 0 {
		return errors.New(managedUsage)
	}
	action := args[0]
	flags := flag.NewFlagSet("managed "+action, flag.ContinueOnError)
	switch action {
	case "serve", "restore":
		return managedServer(action, flags, args[1:])
	case "create", "invite", "accept", "revoke", "mount", "export", "backup":
	default:
		return errors.New(managedUsage)
	}
	endpoint := flags.String("server", "http://127.0.0.1:8484", "managed server URL")
	id := flags.String("drive", "default", "drive name")
	var member, file string
	var writable bool
	switch action {
	case "invite", "revoke":
		flags.StringVar(&member, "member", "", "individual member name")
		if action == "invite" {
			flags.BoolVar(&writable, "write", false, "grant write access")
		}
	case "mount":
		flags.BoolVar(&writable, "write", false, "enable staged writes")
	case "export", "backup":
		flags.StringVar(&file, "file", "", "destination tar archive")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New(managedUsage)
	}
	token := os.Getenv("FREESYNC_MANAGED_TOKEN")
	client, err := managed.NewClient(*endpoint, *id, token)
	if err != nil {
		return err
	}
	if action == "mount" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := client.CheckAccess(ctx); err != nil {
			return err
		}
		var writes *drive.WriteConfig
		if writable {
			config, err := settingsPath()
			if err != nil {
				return err
			}
			spool := fmt.Sprintf("%x", sha256.Sum256([]byte(*endpoint+"\x00"+*id+"\x00"+token)))
			dir := filepath.Join(filepath.Dir(config), "managed-spool", spool)
			dbConfig, err := mountDatabaseConfig(config)
			if err != nil {
				return err
			}
			if dbConfig.Type == database.SQLite && dbConfig.Address == "" {
				dbConfig.Address = filepath.Join(dir, "queue.sqlite")
			}
			writes = &drive.WriteConfig{SpoolDir: dir, Database: dbConfig}
		}
		return drive.Mount(client, writes, cache.Config{})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	method := http.MethodPost
	if action == "backup" || action == "export" {
		method = http.MethodGet
		if file == "" {
			return errors.New("--file is required")
		}
	}
	params := url.Values{}
	if action == "invite" || action == "revoke" {
		if member == "" {
			return errors.New("--member is required")
		}
		params.Set("member", member)
		if writable {
			params.Set("write", "true")
		}
	}
	response, err := client.Request(ctx, method, action, params, nil)
	if err != nil {
		return err
	}
	defer response.Close()
	if file == "" {
		_, err := io.Copy(os.Stdout, response)
		return err
	}
	destination, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, response)
	if err := errors.Join(copyErr, destination.Close()); err != nil {
		return errors.Join(err, os.Remove(file))
	}
	return nil
}

func managedServer(action string, flags *flag.FlagSet, args []string) error {
	dbType := flags.String("db-type", database.SQLite, "database type (sqlite, postgres, or mysql)")
	dbPath := flags.String("db", "", "SQLite file path or database connection string")
	storageFile := flags.String("storage", "", "storage settings file, defaults to storage.json")
	var listen, archive string
	if action == "serve" {
		flags.StringVar(&listen, "listen", "127.0.0.1:8484", "loopback address for the server")
	} else {
		flags.StringVar(&archive, "file", "", "server backup tar archive")
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New(managedUsage)
	}
	settings, err := settingsPath()
	if err != nil {
		return err
	}
	backend, err := database.ParseType(*dbType)
	if err != nil {
		return err
	}
	if *dbPath == "" && backend == database.SQLite {
		*dbPath = filepath.Join(filepath.Dir(settings), "managed.sqlite")
	}
	dbConfig := database.Config{Type: backend, Address: *dbPath}
	if *storageFile == "" {
		*storageFile = filepath.Join(filepath.Dir(settings), "storage.json")
	}
	cfg, err := loadStorageSettings(*storageFile)
	if err != nil {
		return err
	}
	cfg, err = loadStorageSigner(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	contents, err := service.NewStorage(ctx, cfg.StorageConfig)
	if err != nil {
		return err
	}
	if action == "restore" {
		if archive == "" {
			return errors.New("--file is required")
		}
		file, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer file.Close()
		return managed.Restore(ctx, dbConfig, contents, file)
	}
	token := os.Getenv("FREESYNC_MANAGED_ADMIN_TOKEN")
	if len(token) < 32 {
		return errors.New("set FREESYNC_MANAGED_ADMIN_TOKEN to a random secret of at least 32 characters")
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("--listen requires a loopback IP address and port")
	}
	db, err := database.Open(ctx, dbConfig)
	if err != nil {
		return err
	}
	defer db.Close()
	handler := (&managed.Server{DB: db, DatabaseType: backend, Contents: contents, AdminToken: token}).Handler()
	server := &http.Server{Addr: listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	fmt.Fprintf(os.Stderr, "Serving managed drives on %s\n", listener.Addr())
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return errors.Join(err, server.Close())
		}
		return nil
	}
}
