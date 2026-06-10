// Command duo-lsp is a minimal GitLab Duo LSP server that implements the
// LSP 3.18 inline completion protocol backed by the GitLab Code Suggestions API.
//
// Authentication is performed via the OAuth 2.0 Device Authorization Grant flow.
//
// Usage:
//
//	# First-time setup: authenticate and save config
//	duo-lsp auth --client-id <app-id> [--gitlab-url https://gitlab.com]
//
//	# Start the LSP server (reads from stdin, writes to stdout)
//	duo-lsp serve
//
//	# Print the path to the config file
//	duo-lsp config-path
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"gitlab-lsp-client/internal/api"
	"gitlab-lsp-client/internal/auth"
	"gitlab-lsp-client/internal/config"
	"gitlab-lsp-client/internal/lsp"

	"golang.org/x/oauth2"
)

func main() {
	// Parse top-level flags (e.g. --log-level, --log-file) that Neovim or
	// other editors may pass before or instead of a subcommand.
	fs := flag.NewFlagSet("duo-lsp", flag.ContinueOnError)
	logLevel := fs.String("log-level", "", "Log level: debug, info, warn, error")
	logFile := fs.String("log-file", "", "Write logs to this file instead of stderr")
	fs.Usage = printUsage
	if err := fs.Parse(os.Args[1:]); err != nil {
		printUsage()
		os.Exit(1)
	}

	// Build a merged args slice for subcommands, re-injecting any flags that
	// were parsed at the top level so subcommand parsers can see them too.
	var extraFlags []string
	if *logLevel != "" {
		extraFlags = append(extraFlags, "--log-level", *logLevel)
	}
	if *logFile != "" {
		extraFlags = append(extraFlags, "--log-file", *logFile)
	}

	args := fs.Args()
	subcommand := "serve" // default when invoked with no subcommand
	subArgs := extraFlags
	if len(args) > 0 {
		switch args[0] {
		case "auth", "serve", "config-path":
			subcommand = args[0]
			subArgs = append(extraFlags, args[1:]...)
		default:
			// Treat unknown first arg as a flag parse leftover and fall through
			// to serve (handles edge cases like editors passing extra tokens).
			subArgs = append(extraFlags, args...)
		}
	}

	switch subcommand {
	case "auth":
		runAuth(subArgs)
	case "serve":
		runServe(subArgs)
	case "config-path":
		runConfigPath()
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `duo-lsp - GitLab Duo inline completion LSP server

Commands:
  auth        Authenticate via OAuth device flow and save the token
  serve       Start the LSP server (JSON-RPC 2.0 over stdio)
  config-path Print the path to the config file

Run 'duo-lsp <command> -help' for command-specific flags.`)
}

// ---- auth command ----

// defaultClientID is the built-in GitLab OAuth application ID used when the
// caller does not supply --client-id.
const defaultClientID = "9a634432b2fd01bd24437e32ef8ec5cf7b538045253c73a935477e30f78a977f"

func runAuth(args []string) {
	fs := flag.NewFlagSet("auth", flag.ExitOnError)
	clientID := fs.String("client-id", defaultClientID, "OAuth application ID")
	gitlabURL := fs.String("gitlab-url", "https://gitlab.com", "GitLab instance URL")
	_ = fs.Parse(args)

	// Load existing config so we preserve any other settings.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load existing config: %v\n", err)
		cfg = config.Defaults()
	}

	cfg.GitLabBaseURL = *gitlabURL
	cfg.OAuthClientID = *clientID

	ctx := context.Background()
	token, err := auth.DeviceAuth(ctx, cfg.GitLabBaseURL, cfg.OAuthClientID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "authentication failed: %v\n", err)
		os.Exit(1)
	}

	cfg.AccessToken = token.AccessToken
	cfg.RefreshToken = token.RefreshToken
	if !token.Expiry.IsZero() {
		cfg.TokenExpiry = token.Expiry.Format(time.RFC3339)
	}

	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Authentication successful! Token saved.")
	fmt.Println("You can now start the LSP server with: duo-lsp serve")
}

// ---- serve command ----

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	logLevel := fs.String("log-level", "info", "Log level: debug, info, warn, error")
	logFile := fs.String("log-file", "", "Write logs to this file instead of stderr")
	_ = fs.Parse(args)

	// Set up structured logging to stderr (or a file) so it doesn't pollute
	// the stdio LSP channel.
	logWriter := os.Stderr
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "opening log file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		logWriter = f
	}

	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("loading config", "err", err)
		os.Exit(1)
	}

	if err := config.Validate(cfg); err != nil {
		logger.Error("invalid config", "err", err,
			"hint", "run 'duo-lsp auth --client-id <id>' first")
		os.Exit(1)
	}

	if cfg.AccessToken == "" {
		logger.Error("no access token found",
			"hint", "run 'duo-lsp auth --client-id <id>' to authenticate")
		os.Exit(1)
	}

	// Build an oauth2.Token from the stored config so the library can
	// proactively refresh before expiry and handle 401s transparently.
	oauthToken := &oauth2.Token{
		AccessToken:  cfg.AccessToken,
		RefreshToken: cfg.RefreshToken,
	}
	if cfg.TokenExpiry != "" {
		if t, err := time.Parse(time.RFC3339, cfg.TokenExpiry); err == nil {
			oauthToken = oauthToken.WithExtra(nil) // no-op, just for clarity
			oauthToken.Expiry = t
		}
	}

	oauthCfg := &oauth2.Config{
		ClientID: cfg.OAuthClientID,
		Endpoint: oauth2.Endpoint{
			TokenURL: cfg.GitLabBaseURL + "/oauth/token",
		},
	}

	var apiClient *api.Client
	if cfg.RefreshToken != "" {
		apiClient = api.NewClientWithOAuth(
			cfg.GitLabBaseURL,
			oauthCfg,
			oauthToken,
			func(t *oauth2.Token) error {
				cfg.AccessToken = t.AccessToken
				cfg.RefreshToken = t.RefreshToken
				if !t.Expiry.IsZero() {
					cfg.TokenExpiry = t.Expiry.Format(time.RFC3339)
				}
				return config.Save(cfg)
			},
			logger,
		)
	} else {
		logger.Warn("no refresh token stored — re-run 'duo-lsp auth' if the access token expires")
		apiClient = api.NewClient(cfg.GitLabBaseURL, cfg.AccessToken, logger)
	}

	server := lsp.NewServer(os.Stdin, os.Stdout, apiClient, logger)

	logger.Info("duo-lsp server starting",
		"gitlab_url", cfg.GitLabBaseURL,
		"version", "0.1.0",
	)

	if err := server.Run(context.Background()); err != nil {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}

// ---- config-path command ----

func runConfigPath() {
	// Re-use the Load path to derive the config file location.
	// We call Load and then print the path by loading a dummy config.
	// Since config.configPath is unexported, we just document the standard path.
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot determine config dir: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s/duo-lsp/config.json\n", cfgDir)
}
