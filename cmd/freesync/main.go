package main

import (
	"context"
	"errors"
	"flag"
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

func run(args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: freesync daemon --folder PATH")
		return nil
	}
	if len(args) == 0 || args[0] != "daemon" {
		return errors.New("usage: freesync daemon --folder PATH")
	}
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	folder := flags.String("folder", "", "folder to scan")
	port := flags.Int("port", 8384, "loopback HTTP port")
	interval := flags.Duration("scan-interval", 30*time.Second, "time between folder scans")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *folder == "" || *port < 1 || *port > 65535 || *interval <= 0 || flags.NArg() != 0 {
		return errors.New("provide --folder PATH, a valid port, and a positive scan interval")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return daemon.Run(ctx, *folder, *port, *interval)
}
