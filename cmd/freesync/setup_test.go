package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devchux/freesync/internal/database"
)

func TestPromptSettings(t *testing.T) {
	folder := t.TempDir()
	tests := []struct {
		name    string
		input   string
		backend string
		address string
	}{
		{"SQLite defaults", "\n\n", database.SQLite, ""},
		{"SQLite custom file", "sqlite\nstore.sqlite\n", database.SQLite, "store.sqlite"},
		{"PostgreSQL", "postgresql\npostgres://localhost/freesync\n", database.PostgreSQL, "postgres://localhost/freesync"},
		{"MySQL after invalid type", "other\nmysql\nuser@tcp(localhost:3306)/freesync\n", database.MySQL, "user@tcp(localhost:3306)/freesync"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := os.CreateTemp(t.TempDir(), "input")
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			if _, err := fmt.Fprintln(input, folder); err != nil {
				t.Fatal(err)
			}
			if _, err := input.WriteString(tt.input); err != nil {
				t.Fatal(err)
			}
			if _, err := input.Seek(0, 0); err != nil {
				t.Fatal(err)
			}

			var output bytes.Buffer
			cfg, err := promptSettings(input, &output)
			if err != nil {
				t.Fatal(err)
			}
			address := tt.address
			if tt.backend == database.SQLite && address != "" {
				address, err = filepath.Abs(address)
				if err != nil {
					t.Fatal(err)
				}
			}
			want := settings{Folder: folder, Database: database.Config{Type: tt.backend, Address: address}}
			if cfg != want {
				t.Fatalf("got %+v, want %+v", cfg, want)
			}
			folderPrompt := strings.Index(output.String(), "Folder to sync")
			databasePrompt := strings.Index(output.String(), "Database type")
			addressPrompt := strings.LastIndex(output.String(), ": ")
			if folderPrompt < 0 || folderPrompt >= databasePrompt || databasePrompt >= addressPrompt {
				t.Fatalf("unexpected prompt sequence: %q", output.String())
			}

			path := filepath.Join(t.TempDir(), "freesync", "settings.json")
			if err := writeSettings(path, cfg); err != nil {
				t.Fatal(err)
			}
			saved, err := readSettings(path)
			if err != nil {
				t.Fatal(err)
			}
			if saved != want {
				t.Fatalf("saved %+v, want %+v", saved, want)
			}
		})
	}
}
