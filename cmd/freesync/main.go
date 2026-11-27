package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/devchux/freesync/internal/daemon"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func start(cfg settings, port int, interval time.Duration, save func() error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return daemon.Run(ctx, cfg.Folder, port, interval, cfg.Database, save)
}

const (
	usage           = "Usage: freesync [setup | storage setup | storage list | storage mount | daemon --folder PATH [--db-type TYPE] [--db ADDRESS]]"
	defaultPort     = 8384
	defaultInterval = 30 * time.Second
)

func run(args []string) error {
	switch {
	case len(args) == 0:
		return runSetup(false)
	case len(args) == 1 && args[0] == "setup":
		return runSetup(true)
	case len(args) == 1 && (args[0] == "--help" || args[0] == "-h"):
		fmt.Println(usage)
		return nil
	case len(args) == 2 && args[0] == "storage":
		return runStorage(args[1])
	case args[0] == "daemon":
		return runDaemon(args[1:])
	default:
		return errors.New(usage)
	}
}
