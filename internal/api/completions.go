// Package api wraps the GitLab Code Suggestions REST API.
// Endpoint: POST /api/v4/code_suggestions/completions
// Docs: https://docs.gitlab.com/ee/api/code_suggestions.html
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

const (
	completionsPath = "/api/v4/code_suggestions/completions"
	// promptVersion matches what gitlab-lsp sends.
	promptVersion = 1
)

// Client calls the GitLab Code Suggestions API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates a new API client using a plain static access token.
// Use NewClientWithOAuth for automatic token refresh support.
func NewClient(baseURL, accessToken string, logger *slog.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &bearerTransport{
				token: accessToken,
				base:  http.DefaultTransport,
			},
		},
		logger: logger,
	}
}

// NewClientWithOAuth creates an API client whose HTTP transport is backed by
// the oauth2 library. Tokens are refreshed automatically when they expire.
// onTokenSaved is called with the new token after each refresh so the caller
// can persist it to disk.
func NewClientWithOAuth(
	baseURL string,
	oauthCfg *oauth2.Config,
	token *oauth2.Token,
	onTokenSaved func(*oauth2.Token) error,
	logger *slog.Logger,
) *Client {
	ts := &persistingTokenSource{
		base:         oauthCfg.TokenSource(context.Background(), token),
		onTokenSaved: onTokenSaved,
		logger:       logger,
	}
	httpClient := oauth2.NewClient(context.Background(), ts)
	httpClient.Timeout = 30 * time.Second
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
		logger:     logger,
	}
}

// persistingTokenSource wraps an oauth2.TokenSource and calls onTokenSaved
// whenever the underlying source issues a new token (i.e. after a refresh).
type persistingTokenSource struct {
	base         oauth2.TokenSource
	onTokenSaved func(*oauth2.Token) error
	logger       *slog.Logger
	last         string // last seen access token, to detect refreshes
}

func (s *persistingTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.base.Token()
	if err != nil {
		return nil, err
	}
	if token.AccessToken != s.last {
		s.logger.Info("oauth2 token refreshed, persisting to disk")
		if saveErr := s.onTokenSaved(token); saveErr != nil {
			s.logger.Warn("failed to persist refreshed token", "err", saveErr)
		}
		s.last = token.AccessToken
	}
	return token, nil
}

// bearerTransport injects a static Bearer token into every request.
// Used by NewClient when no OAuth refresh is needed.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	r.Header.Set("X-Gitlab-Authentication-Type", "oauth")
	return t.base.RoundTrip(r)
}

// CompletionRequest mirrors the payload sent by gitlab-lsp.
type CompletionRequest struct {
	PromptVersion int         `json:"prompt_version"`
	ProjectPath   string      `json:"project_path"`
	ProjectID     int         `json:"project_id"`
	CurrentFile   CurrentFile `json:"current_file"`
	Intent        string      `json:"intent,omitempty"`
	ChoicesCount  int         `json:"choices_count,omitempty"`
}

// CurrentFile holds the document context around the cursor.
type CurrentFile struct {
	FileName           string `json:"file_name"`
	ContentAboveCursor string `json:"content_above_cursor"`
	ContentBelowCursor string `json:"content_below_cursor"`
}

// CompletionResponse is the API response.
type CompletionResponse struct {
	Choices []Choice `json:"choices"`
	Model   *Model   `json:"model,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// Choice is a single completion option.
type Choice struct {
	Text string `json:"text"`
}

// Model describes the model that produced the completion.
type Model struct {
	Engine string `json:"engine,omitempty"`
	Name   string `json:"name,omitempty"`
	Lang   string `json:"lang,omitempty"`
}

// GetCompletions calls the completions endpoint and returns the response.
// Token refresh (when using NewClientWithOAuth) is handled transparently by
// the underlying oauth2 HTTP transport.
func (c *Client) GetCompletions(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	req.PromptVersion = promptVersion

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	url := c.baseURL + completionsPath
	c.logger.Debug("api →",
		"method", http.MethodPost,
		"url", url,
		"body", string(body),
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start)
	if err != nil {
		c.logger.Debug("api error", "url", url, "elapsed", elapsed, "err", err)
		return nil, fmt.Errorf("completions request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	c.logger.Debug("api ←",
		"status", resp.StatusCode,
		"elapsed", elapsed,
		"body", string(respBody),
	)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("completions API returned %d: %s", resp.StatusCode, string(respBody))
	}

        if len(respBody) == 0 {
                return nil, fmt.Errorf("empty response body")
        }

	var result CompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return &result, nil
}
