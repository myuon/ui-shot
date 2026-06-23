package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// Config is the global configuration persisted as TOML.
type Config struct {
	Version  int            `toml:"version"`
	Provider string         `toml:"provider"`
	Defaults DefaultsConfig `toml:"defaults"`
	GCS      GCSConfig      `toml:"gcs"`
	S3       S3Config       `toml:"s3"`
	R2       R2Config       `toml:"r2"`
}

// DefaultsConfig holds provider-agnostic defaults.
type DefaultsConfig struct {
	RepoPrefixMode string `toml:"repo_prefix_mode"`
}

// GCSConfig holds the GCS provider settings.
type GCSConfig struct {
	ProjectID string `toml:"project_id"`
	Bucket    string `toml:"bucket"`
	BaseURL   string `toml:"base_url"`
}

// S3Config holds the S3 provider settings (designed, not implemented).
type S3Config struct {
	Bucket  string `toml:"bucket"`
	Region  string `toml:"region"`
	BaseURL string `toml:"base_url"`
	Profile string `toml:"profile"`
}

// R2Config holds the R2 provider settings (designed, not implemented).
type R2Config struct {
	Bucket    string `toml:"bucket"`
	BaseURL   string `toml:"base_url"`
	AccountID string `toml:"account_id"`
}

// Default returns a config populated with sensible defaults.
func Default() *Config {
	return &Config{
		Version: 1,
		Defaults: DefaultsConfig{
			RepoPrefixMode: "git_remote",
		},
		S3: S3Config{Profile: "default"},
	}
}

// Path returns the path to the global config file.
//
//   - Windows: %APPDATA%\uishot\config.toml
//   - macOS/Linux: ~/.config/uishot/config.toml
func Path() (string, error) {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "uishot", "config.toml"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "uishot", "config.toml"), nil
}

// Load reads the config from the given path. If the file does not exist a
// default config is returned with exists=false.
func Load(path string) (cfg *Config, exists bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), false, nil
		}
		return nil, false, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg = Default()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, false, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, true, nil
}

// Save writes the config to the given path, creating parent directories.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open config for writing: %w", err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return nil
}
