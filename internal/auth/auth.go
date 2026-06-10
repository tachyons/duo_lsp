// Package auth wraps golang.org/x/oauth2 device auth flow for GitLab.
package auth

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
)

// GitLabScopes are the minimum OAuth scopes needed for code suggestions.
const GitLabScopes = "api read_user"

// DeviceAuth performs the OAuth 2.0 Device Authorization Grant against a
// GitLab instance and returns the resulting token.
//
// It prints the user_code and verification_uri to stdout so the user can
// open a browser on any device to complete authentication.
func DeviceAuth(ctx context.Context, baseURL, clientID string) (*oauth2.Token, error) {
	cfg := &oauth2.Config{
		ClientID: clientID,
		Scopes:   []string{"api", "read_user"},
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: baseURL + "/oauth/authorize_device",
			TokenURL:      baseURL + "/oauth/token",
		},
	}

	// Step 1: request a device code.
	resp, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("device auth request: %w", err)
	}

	// Step 2: prompt the user.
	fmt.Printf("\nTo authenticate, visit:\n\n  %s\n\nand enter code: %s\n\n",
		resp.VerificationURI, resp.UserCode)
	if resp.VerificationURIComplete != "" {
		fmt.Printf("Or open the complete URL directly:\n\n  %s\n\n", resp.VerificationURIComplete)
	}
	fmt.Println("Waiting for authorization...")

	// Step 3: poll until approved or expired.
	token, err := cfg.DeviceAccessToken(ctx, resp)
	if err != nil {
		return nil, fmt.Errorf("polling for token: %w", err)
	}

	return token, nil
}

// RefreshToken exchanges a refresh token for a new access token using the
// OAuth 2.0 refresh token grant against the given GitLab instance.
func RefreshToken(ctx context.Context, baseURL, clientID, refreshToken string) (*oauth2.Token, error) {
	cfg := &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			TokenURL: baseURL + "/oauth/token",
		},
	}

	// Seed with an expired token that carries only the refresh token; the
	// oauth2 library will immediately perform the refresh grant.
	seed := &oauth2.Token{RefreshToken: refreshToken}
	token, err := cfg.TokenSource(ctx, seed).Token()
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	return token, nil
}
