// Package lsp implements a minimal LSP 3.18 server that serves inline
// completions backed by the GitLab Code Suggestions API.
//
// Transport: JSON-RPC 2.0 over stdio (Content-Length framed).
// The server is intentionally single-threaded for simplicity; requests are
// processed sequentially in the order they arrive.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gitlab-lsp-client/internal/api"
)

const (
	serverName    = "duo-lsp"
	serverVersion = "0.1.0"

	// LSP TextDocumentSyncKind.Full = 1
	syncKindFull = 1

	// InlineCompletionTriggerKind: 1 = Invoked, 2 = Automatic
	triggerKindInvoked   = 1
	triggerKindAutomatic = 2
)

// inflightReq tracks a single in-flight inlineCompletion request.
type inflightReq struct {
	id     interface{}
	cancel context.CancelFunc
}

// document holds the current state of an open text document.
type document struct {
	uri        string
	languageID string
	version    int
	text       string
}

// Server is the LSP server.
type Server struct {
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex // guards writer

	apiClient *api.Client
	logger    *slog.Logger

	// documents tracks open text documents by URI.
	documents map[string]*document

	// inflight tracks the request ID of the most recent in-flight
	// inlineCompletion request per document URI, and its cancel func.
	// Guarded by mu.
	inflight map[string]*inflightReq

	// initialized is set to true after the initialize handshake completes.
	initialized bool

	// shutdown is set to true after the client sends "shutdown".
	shutdown bool
}

// NewServer creates a new Server that reads from r and writes to w.
func NewServer(r io.Reader, w io.Writer, apiClient *api.Client, logger *slog.Logger) *Server {
	return &Server{
		reader:    bufio.NewReader(r),
		writer:    w,
		apiClient: apiClient,
		logger:    logger,
		documents: make(map[string]*document),
		inflight:  make(map[string]*inflightReq),
	}
}

// Run reads JSON-RPC messages from stdin and dispatches them until EOF or
// the "exit" notification is received.
func (s *Server) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := s.readMessage()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			s.logger.Error("reading message", "err", err)
			continue
		}

		s.dispatch(ctx, msg)
	}
}

// ---- Transport ----

// readMessage reads one Content-Length framed JSON-RPC message.
func (s *Server) readMessage() (json.RawMessage, error) {
	var contentLength int

	// Read headers until blank line.
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			n, err := strconv.Atoi(strings.TrimPrefix(line, "Content-Length: "))
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %w", err)
			}
			contentLength = n
		}
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(s.reader, buf); err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}
	return json.RawMessage(buf), nil
}

// writeMessage sends a Content-Length framed JSON-RPC message.
func (s *Server) writeMessage(v interface{}) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshalling message: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = fmt.Fprintf(s.writer, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

// sendResponse sends a successful JSON-RPC response.
// result is marshalled to JSON; a nil result is sent as JSON null so that
// the response always contains a "result" field, as required by JSON-RPC 2.0.
func (s *Server) sendResponse(id interface{}, result interface{}) {
	encoded, err := json.Marshal(result)
	if err != nil {
		s.logger.Error("marshalling result", "err", err)
		s.sendError(id, InternalError, "internal error")
		return
	}
	s.logger.Debug("rpc ←",
		"id", id,
		"result", string(encoded),
	)
	if err := s.writeMessage(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  json.RawMessage(encoded),
	}); err != nil {
		s.logger.Error("sending response", "err", err)
	}
}

// sendError sends a JSON-RPC error response.
func (s *Server) sendError(id interface{}, code int, message string) {
	s.logger.Debug("rpc ← error",
		"id", id,
		"code", code,
		"message", message,
	)
	if err := s.writeMessage(Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &ResponseError{Code: code, Message: message},
	}); err != nil {
		s.logger.Error("sending error", "err", err)
	}
}

// ---- Dispatch ----

// dispatch routes an incoming raw message to the appropriate handler.
func (s *Server) dispatch(ctx context.Context, raw json.RawMessage) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		s.logger.Error("parsing message", "err", err)
		s.sendError(nil, ParseError, "parse error")
		return
	}

	s.logger.Debug("rpc →",
		"method", req.Method,
		"id", req.ID,
		"params", string(req.Params),
	)

	// Notifications have no ID.
	isNotification := req.ID == nil

	switch req.Method {
	case "initialize":
		s.handleInitialize(ctx, req)
	case "initialized":
		// Client notification – no response needed.
		s.initialized = true
	case "shutdown":
		s.shutdown = true
		s.sendResponse(req.ID, nil)
	case "exit":
		code := 0
		if !s.shutdown {
			code = 1
		}
		os.Exit(code)

	// Text document sync
	case "textDocument/didOpen":
		s.handleDidOpen(req)
	case "textDocument/didChange":
		s.handleDidChange(req)
	case "textDocument/didClose":
		s.handleDidClose(req)

	// Inline completion (LSP 3.18) – run in a goroutine so the read loop
	// is not blocked while waiting for the API response.
	case "textDocument/inlineCompletion":
		go s.handleInlineCompletion(ctx, req)

	default:
		if !isNotification {
			s.sendError(req.ID, MethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
		}
	}
}

// ---- Handlers ----

func (s *Server) handleInitialize(_ context.Context, req Request) {
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, InvalidParams, "invalid initialize params")
		return
	}

	inlineCompletionSupported := params.Capabilities.TextDocument.InlineCompletion != nil
	s.logger.Info("initialize",
		"client", params.ClientInfo,
		"rootUri", params.RootURI,
		"inline_completion_capability", inlineCompletionSupported,
	)
	if !inlineCompletionSupported {
		s.logger.Warn("client did not declare textDocument.inlineCompletion capability — " +
			"textDocument/inlineCompletion requests will not be sent by the client")
	}

	result := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync:         syncKindFull,
			InlineCompletionProvider: &InlineCompletionOptions{},
		},
		ServerInfo: &ServerInfo{
			Name:    serverName,
			Version: serverVersion,
		},
	}
	s.sendResponse(req.ID, result)
}

func (s *Server) handleDidOpen(req Request) {
	var params DidOpenTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.logger.Error("didOpen params", "err", err)
		return
	}
	s.documents[params.TextDocument.URI] = &document{
		uri:        params.TextDocument.URI,
		languageID: params.TextDocument.LanguageID,
		version:    params.TextDocument.Version,
		text:       params.TextDocument.Text,
	}
	s.logger.Debug("opened", "uri", params.TextDocument.URI)
}

func (s *Server) handleDidChange(req Request) {
	var params DidChangeTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.logger.Error("didChange params", "err", err)
		return
	}
	doc, ok := s.documents[params.TextDocument.URI]
	if !ok {
		s.logger.Warn("didChange for unknown document", "uri", params.TextDocument.URI)
		return
	}
	doc.version = params.TextDocument.Version
	// Full sync: last change event contains the whole document.
	if len(params.ContentChanges) > 0 {
		doc.text = params.ContentChanges[len(params.ContentChanges)-1].Text
	}
}

func (s *Server) handleDidClose(req Request) {
	var params DidCloseTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.logger.Error("didClose params", "err", err)
		return
	}
	delete(s.documents, params.TextDocument.URI)
	s.logger.Debug("closed", "uri", params.TextDocument.URI)
}

func (s *Server) handleInlineCompletion(ctx context.Context, req Request) {
	var params InlineCompletionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, InvalidParams, "invalid inlineCompletion params")
		return
	}

	uri := params.TextDocument.URI

	// Cancel any previous in-flight request for this document, then register
	// a cancel for this one. This ensures only the latest request per URI
	// reaches the API, preventing stale responses from being shown.
	reqCtx, cancel := context.WithCancel(ctx)
	thisReq := &inflightReq{id: req.ID, cancel: cancel}

	s.mu.Lock()
	if prev, ok := s.inflight[uri]; ok {
		prev.cancel()
	}
	s.inflight[uri] = thisReq
	doc, ok := s.documents[uri]
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		// Only clean up if this request is still the current one.
		if s.inflight[uri] == thisReq {
			delete(s.inflight, uri)
		}
		s.mu.Unlock()
		cancel()
	}()

	if !ok {
		// Unknown document – return empty list.
		s.sendResponse(req.ID, InlineCompletionList{Items: []InlineCompletionItem{}})
		return
	}

	prefix, suffix := splitAtCursor(doc.text, params.Position)

	s.logger.Debug("inline completion context",
		"uri", params.TextDocument.URI,
		"prefix_len", len(prefix),
		"suffix_len", len(suffix),
		"prefix_trimmed_len", len(strings.TrimSpace(prefix)),
	)

	// Skip if there's too little content to be useful.
	const minContentLen = 3
	if len(strings.TrimSpace(prefix))+len(strings.TrimSpace(suffix)) < minContentLen {
		s.logger.Debug("skipping completion: content too short",
			"total_trimmed", len(strings.TrimSpace(prefix))+len(strings.TrimSpace(suffix)),
			"min", minContentLen,
		)
		s.sendResponse(req.ID, InlineCompletionList{Items: []InlineCompletionItem{}})
		return
	}

	fileName := filepath.Base(params.TextDocument.URI)

	apiReq := api.CompletionRequest{
		ProjectPath: "",
		ProjectID:   -1,
		CurrentFile: api.CurrentFile{
			FileName:           fileName,
			ContentAboveCursor: prefix,
			ContentBelowCursor: suffix,
		},
		Intent:       "completion",
		ChoicesCount: 3,
	}

	s.logger.Debug("requesting completion",
		"uri", params.TextDocument.URI,
		"line", params.Position.Line,
		"char", params.Position.Character,
	)

	apiResp, err := s.apiClient.GetCompletions(reqCtx, apiReq)
	if err != nil {
		if reqCtx.Err() != nil {
			s.logger.Debug("completion cancelled (superseded by newer request)",
				"uri", uri, "id", req.ID)
			return // do not send a response for a cancelled request
		}
		s.logger.Error("completions API error", "err", err)
		// Return empty list rather than an error so the editor stays responsive.
		s.sendResponse(req.ID, InlineCompletionList{Items: []InlineCompletionItem{}})
		return
	}

	// A zero-length range at the cursor position. This is required so that
	// Neovim's inline_completion handler uses item.range instead of falling
	// back to vim.fn.bufwinid(), which returns -1 for non-visible buffers
	// and causes a "Invalid 'buffer'" crash in pos.lua:213.
	cursorRange := &Range{
		Start: params.Position,
		End:   params.Position,
	}

	items := make([]InlineCompletionItem, 0, len(apiResp.Choices))
	for _, choice := range apiResp.Choices {
		if choice.Text == "" {
			continue
		}
		items = append(items, InlineCompletionItem{
			InsertText: NewPlainInsertText(choice.Text),
			Range:      cursorRange,
		})
	}

	s.sendResponse(req.ID, InlineCompletionList{Items: items})
}

// ---- Helpers ----

// splitAtCursor splits the document text into prefix (content above/at cursor)
// and suffix (content below cursor) based on the LSP Position.
func splitAtCursor(text string, pos Position) (prefix, suffix string) {
	lines := strings.Split(text, "\n")

	if pos.Line >= len(lines) {
		return text, ""
	}

	// Collect all lines before the cursor line.
	var sb strings.Builder
	for i := 0; i < pos.Line; i++ {
		sb.WriteString(lines[i])
		sb.WriteByte('\n')
	}

	// Add the partial cursor line up to the character position.
	cursorLine := lines[pos.Line]
	charPos := pos.Character
	if charPos > len(cursorLine) {
		charPos = len(cursorLine)
	}
	sb.WriteString(cursorLine[:charPos])
	prefix = sb.String()

	// Suffix: rest of cursor line + remaining lines.
	var sb2 strings.Builder
	sb2.WriteString(cursorLine[charPos:])
	for i := pos.Line + 1; i < len(lines); i++ {
		sb2.WriteByte('\n')
		sb2.WriteString(lines[i])
	}
	suffix = sb2.String()

	return prefix, suffix
}
