# duo-lsp

A minimal, self-contained GitLab Duo LSP server written in Go.

Implements the **LSP 3.18 inline completion protocol** (`textDocument/inlineCompletion`)
backed by the [GitLab Code Suggestions API](https://docs.gitlab.com/ee/api/code_suggestions.html).

Authentication uses the **OAuth 2.0 Device Authorization Grant** flow — no browser
required on the machine running the server.

## Prerequisites

- A GitLab account with Duo Code Suggestions enabled

## Installation

### Homebrew (macOS and Linux)

```sh
brew tap tachyons/duo-lsp https://gitlab.com/tachyons/duo_mini_lsp
brew install duo-lsp
```

The formula lives in this repo under `Formula/duo-lsp.rb` and is updated
automatically on every release.

### Build from source

Requires Go 1.22+.

```sh
go mod tidy
go build -o duo-lsp ./cmd/duo-lsp
```

## Authenticate

```sh
# Uses the built-in OAuth application ID — no flags needed for gitlab.com
./duo-lsp auth

# For a self-managed GitLab instance, or to use your own OAuth application:
./duo-lsp auth --gitlab-url https://gitlab.example.com
./duo-lsp auth --client-id <your-application-id>
```

The command prints a URL and a user code. Open the URL in any browser, enter the
code, and approve the request. The token is saved automatically.

## Start the LSP Server

```sh
./duo-lsp serve
```

The server reads JSON-RPC 2.0 messages from **stdin** and writes responses to
**stdout** (Content-Length framed, as per the LSP spec). Logs go to **stderr**.

Optional flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |
| `--log-file` | *(stderr)* | Write logs to a file instead |

## Editor Integration

### Neovim

Requires Neovim **0.12+**. The built-in `vim.lsp` and `vim.lsp.inline_completion`
APIs are used — no plugins required.

#### 1. Register and enable the server

Add to your `init.lua` (or any file sourced at startup):

```lua
vim.lsp.config['gitlab_duo_lsp'] = {
  cmd = { "/path/to/duo-lsp", "serve" },
  -- Add or remove filetypes as needed
  filetypes = { "ruby", "go", "lua", "python", "javascript", "typescript" },
}

vim.lsp.enable('gitlab_duo_lsp')
```

Replace `/path/to/duo-lsp` with the absolute path to the binary. If installed
via Homebrew, use `vim.fn.exepath('duo-lsp')` instead of a hardcoded path.

#### 2. Enable inline completion

```lua
vim.lsp.inline_completion.enable()
```

This activates ghost-text suggestions that appear automatically while you type
(debounced at 200 ms by Neovim's built-in handler).

#### 3. Accept a suggestion with Tab

```lua
vim.keymap.set('i', '<Tab>', function()
  if not vim.lsp.inline_completion.get() then
    return '<Tab>'
  end
end, { expr = true, desc = 'Accept the current inline completion' })
```

`<Tab>` accepts the displayed suggestion when one is visible, and falls back to
a literal tab otherwise.

#### Complete minimal config

```lua
-- ~/.config/nvim/init.lua  (or any sourced file)

vim.lsp.config['gitlab_duo_lsp'] = {
  cmd = { vim.fn.exepath('duo-lsp') ~= '' and vim.fn.exepath('duo-lsp') or '/path/to/duo-lsp', 'serve' },
  filetypes = { 'ruby', 'go', 'lua', 'python', 'javascript', 'typescript' },
}

vim.lsp.enable('gitlab_duo_lsp')
vim.lsp.inline_completion.enable()

-- Tab: accept suggestion or fall back to literal tab
vim.keymap.set('i', '<Tab>', function()
  if not vim.lsp.inline_completion.get() then
    return '<Tab>'
  end
end, { expr = true, desc = 'Accept the current inline completion' })

-- Optional: cycle through multiple suggestions
vim.keymap.set('i', '<M-]>', function()
  vim.lsp.inline_completion.select({ count = 1 })
end, { desc = 'Next inline completion' })

vim.keymap.set('i', '<M-[>', function()
  vim.lsp.inline_completion.select({ count = -1 })
end, { desc = 'Previous inline completion' })
```

#### Optional: debug logging

```lua
vim.lsp.config['gitlab_duo_lsp'] = {
  cmd = { 'duo-lsp', 'serve', '--log-level', 'debug', '--log-file', '/tmp/duo-lsp.log' },
  filetypes = { 'ruby', 'go', 'lua' },
}
```

```sh
tail -f /tmp/duo-lsp.log
```

## Architecture

```
cmd/duo-lsp/main.go          CLI entry point (auth / serve / config-path)
internal/
  auth/auth.go               OAuth device flow (wraps golang.org/x/oauth2)
  config/config.go           Persistent JSON config (~/.config/duo-lsp/config.json)
  api/completions.go         GitLab Code Suggestions REST client
  lsp/
    types.go                 LSP 3.18 protocol types (inline completion subset)
    server.go                JSON-RPC 2.0 / LSP server (stdio transport)
```

## LSP Methods Handled

| Method | Description |
|--------|-------------|
| `initialize` | Handshake; advertises `inlineCompletionProvider` |
| `initialized` | Client confirmation notification |
| `shutdown` / `exit` | Clean shutdown |
| `textDocument/didOpen` | Track document content |
| `textDocument/didChange` | Update document content (full sync) |
| `textDocument/didClose` | Remove document from cache |
| `textDocument/inlineCompletion` | **Return inline completions from GitLab API** |

## Config File

Located at `~/.config/duo-lsp/config.json` (macOS/Linux) or
`%AppData%\duo-lsp\config.json` (Windows).

```json
{
  "gitlab_base_url": "https://gitlab.com",
  "oauth_client_id": "your-app-id",
  "access_token": "<stored after auth>",
  "refresh_token": "<stored after auth>"
}
```
