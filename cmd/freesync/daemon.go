package main

import (
	"errors"
	"flag"

	"github.com/devchux/freesync/internal/database"
)

func runDaemon(args []string) error {
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	folder := flags.String("folder", "", "folder to scan")
	dbType := flags.String("db-type", database.SQLite, "database type (sqlite, postgres, or mysql)")
	dbAddress := flags.String("db", "", "SQLite file path or database connection string")
	port := flags.Int("port", defaultPort, "loopback HTTP port")
	interval := flags.Duration("scan-interval", defaultInterval, "time between folder scans")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *folder == "" || *port < 1 || *port > 65535 || *interval <= 0 || flags.NArg() != 0 {
		return errors.New("provide --folder PATH, a valid port, and a positive scan interval")
	}
	backend, err := database.ParseType(*dbType)
	if err != nil {
		return err
	}
	return start(settings{
		Folder:   *folder,
		Database: database.Config{Type: backend, Address: *dbAddress},
	}, *port, *interval, nil)
}
