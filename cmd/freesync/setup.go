package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/devchux/freesync/internal/database"
	"golang.org/x/term"
)

type settings struct {
	Folder   string          `json:"folder"`
	Database database.Config `json:"database"`
}

func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "freesync", "settings.json"), nil
}

func readSettings(path string) (settings, error) {
	var cfg settings
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("read settings: %w", err)
	}
	cfg.Database.Type, err = database.ParseType(cfg.Database.Type)
	return cfg, err
}

func writeSettings(path string, cfg settings) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	writeErr := json.NewEncoder(file).Encode(cfg)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func promptSettings(input *os.File, output io.Writer) (settings, error) {
	var cfg settings
	reader := bufio.NewReader(input)
	ask := func(label string, secret bool) (string, error) {
		if _, err := fmt.Fprint(output, label); err != nil {
			return "", err
		}
		if secret && term.IsTerminal(int(input.Fd())) {
			value, err := term.ReadPassword(int(input.Fd()))
			fmt.Fprintln(output)
			return strings.TrimSpace(string(value)), err
		}
		value, err := reader.ReadString('\n')
		if err == io.EOF && len(value) > 0 {
			err = nil
		}
		return strings.TrimSpace(value), err
	}

	for {
		folder, err := ask("Folder to sync: ", false)
		if err != nil {
			return cfg, err
		}
		if folder != "" {
			folder, err = filepath.Abs(folder)
			if err == nil {
				info, statErr := os.Stat(folder)
				if statErr == nil && info.IsDir() {
					cfg.Folder = folder
					break
				}
			}
		}
		fmt.Fprintln(output, "Enter a folder that FreeSync can open.")
	}

	for {
		value, err := ask("Database type (SQLite, PostgreSQL, MySQL) [SQLite]: ", false)
		if err != nil {
			return cfg, err
		}
		cfg.Database.Type, err = database.ParseType(value)
		if err == nil {
			break
		}
		fmt.Fprintln(output, err)
	}

	switch cfg.Database.Type {
	case database.SQLite:
		path, err := ask("SQLite file path [default location]: ", false)
		if err != nil {
			return cfg, err
		}
		if path != "" {
			cfg.Database.Address, err = filepath.Abs(path)
			if err != nil {
				return cfg, err
			}
		}
	case database.PostgreSQL, database.MySQL:
		label := "PostgreSQL connection string (postgres://user:password@host:5432/database): "
		if cfg.Database.Type == database.MySQL {
			label = "MySQL connection string (user:password@tcp(host:3306)/database): "
		}
		for cfg.Database.Address == "" {
			value, err := ask(label, true)
			if err != nil {
				return cfg, err
			}
			cfg.Database.Address = value
		}
	}
	return cfg, nil
}

func runSetup(forcePrompt bool) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if !forcePrompt {
		cfg, err := readSettings(path)
		if err == nil {
			return start(cfg, defaultPort, defaultInterval, nil)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	cfg, err := promptSettings(os.Stdin, os.Stderr)
	if err != nil {
		return fmt.Errorf("first-run setup needs a folder and database settings: %w", err)
	}
	return start(cfg, defaultPort, defaultInterval, func() error {
		return writeSettings(path, cfg)
	})
}
