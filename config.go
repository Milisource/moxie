package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/mili/moxie/internal/db"
)

// configDir returns the platform-standard configuration directory for moxie.
func configDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "moxie")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "moxie")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "moxie")
	}
}

// dbPath returns the path to the games database.
func dbPath() string {
	dir := configDir()
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "games.db")
}

// configPath returns the path to the JSON config file.
func configPath() string {
	dir := configDir()
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "config.json")
}

// readConfig reads the JSON config file. Returns an empty map if the file
// does not exist.
func readConfig() (map[string]string, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	var cfg map[string]string
	if err := json.Unmarshal(data, &cfg); err != nil {
		return make(map[string]string), nil // corrupted → start fresh
	}
	if cfg == nil {
		return make(map[string]string), nil
	}
	return cfg, nil
}

// writeConfig serializes the config map as JSON and writes it to the config
// file atomically.
func writeConfig(cfg map[string]string) error {
	path := configPath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	return os.Rename(tmpName, path)
}

func openDB() *db.Database {
	path := dbPath()
	database, err := db.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	return database
}

func mustParseInt(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid ID: %s\n", s)
		os.Exit(1)
	}
	return n
}
