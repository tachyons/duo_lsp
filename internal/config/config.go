// Package config handles persistent configuration (token storage, settings).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configDirName  = "duo-lsp"
	configFileName = "config.json"
)

// Config holds all persisted settings for the client.
type Config struct {
	// GitLabBaseURL is the GitLab instance URL, e.g. "https://gitlab.com".
	GitLabBaseURL string `json:"gitlab_base_url"`
	// OAuthClientID is the registered OAuth application ID.
	OAuthClientID string `json:"oauth_client_id"`
	// AccessToken is the stored OAuth access token (written after device auth).
	AccessToken string `json:"access_token,omitempty"`
	// RefreshToken is the OAuth refresh token (if provided).
	RefreshToken string `json:"refresh_token,omitempty"`
	// TokenExpiry is the expiry time of the access token in RFC3339 format.
	// Used by the oauth2 library to proactively refresh before expiry.
	TokenExpiry string `json:"token_expiry,omitempty"`
}

// Defaults returns a Config pre-filled with sensible defaults.
func Defaults() Config {
	return Config{
		GitLabBaseURL: "https://gitlab.com",
	}
}

// configPath returns the OS-appropriate path for the config file.
func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config dir: %w", err)
	}
	return filepath.Join(dir, configDirName, configFileName), nil
}

// Load reads the config from disk. Returns Defaults() if the file doesn't exist.
func Load() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Defaults(), err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), fmt.Errorf("reading config: %w", err)
	}

	cfg := Defaults()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Defaults(), fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

// Save writes the config to disk, creating directories as needed.
func Save(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// Validate returns an error if required fields are missing.
func Validate(cfg Config) error {
	if cfg.GitLabBaseURL == "" {
		return errors.New("gitlab_base_url is required")
	}
	if cfg.OAuthClientID == "" {
		return errors.New("oauth_client_id is required (register an OAuth app at <gitlab>/oauth/applications)")
	}
	return nil
}
