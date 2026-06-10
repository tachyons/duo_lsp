// Package lsp contains LSP 3.18 protocol types used by the server.
// Only the subset needed for inline completions is defined here.
// Spec: https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/
package lsp

import "encoding/json"

// ---- JSON-RPC 2.0 ----

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// ResponseError is the error object in a JSON-RPC 2.0 response.
type ResponseError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *ResponseError) Error() string { return e.Message }

// JSON-RPC error codes.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// ---- LSP 3.18 Initialize ----

// InitializeParams is sent as the first request to the server.
type InitializeParams struct {
	ProcessID             *int               `json:"processId"`
	ClientInfo            *ClientInfo        `json:"clientInfo,omitempty"`
	RootURI               *string            `json:"rootUri"`
	WorkspaceFolders      []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	InitializationOptions interface{}        `json:"initializationOptions,omitempty"`
}

// ClientInfo identifies the client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// WorkspaceFolder represents an open workspace folder.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// ClientCapabilities declares what the client supports.
type ClientCapabilities struct {
	TextDocument TextDocumentClientCapabilities `json:"textDocument,omitempty"`
	Workspace    WorkspaceClientCapabilities    `json:"workspace,omitempty"`
}

// TextDocumentClientCapabilities holds text-document-level capabilities.
type TextDocumentClientCapabilities struct {
	Synchronization  *TextDocumentSyncClientCapabilities `json:"synchronization,omitempty"`
	InlineCompletion *InlineCompletionClientCapabilities `json:"inlineCompletion,omitempty"`
}

// TextDocumentSyncClientCapabilities declares sync support.
type TextDocumentSyncClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	DidSave             bool `json:"didSave,omitempty"`
}

// InlineCompletionClientCapabilities declares inline completion support (LSP 3.18).
type InlineCompletionClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// WorkspaceClientCapabilities holds workspace-level capabilities.
type WorkspaceClientCapabilities struct {
	WorkspaceFolders bool `json:"workspaceFolders,omitempty"`
}

// InitializeResult is the server's response to initialize.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

// ServerCapabilities declares what the server supports.
type ServerCapabilities struct {
	// TextDocumentSync: 1 = Full, 2 = Incremental.
	TextDocumentSync         int                      `json:"textDocumentSync"`
	InlineCompletionProvider *InlineCompletionOptions `json:"inlineCompletionProvider,omitempty"`
}

// InlineCompletionOptions is the server-side inline completion registration options.
// Kept empty; no resolve provider or work-done progress needed for this minimal server.
type InlineCompletionOptions struct{}

// ServerInfo identifies the server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ---- LSP Text Document Sync ----

// DidOpenTextDocumentParams is sent when a document is opened.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// TextDocumentItem is a document with its content.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// DidChangeTextDocumentParams is sent when a document changes.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// VersionedTextDocumentIdentifier identifies a versioned document.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentContentChangeEvent is a full-document change event.
// (TextDocumentSyncKind.Full – the server requests the whole text each time.)
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidCloseTextDocumentParams is sent when a document is closed.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// TextDocumentIdentifier identifies a document by URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// ---- LSP 3.18 Inline Completion ----

// InlineCompletionParams is the request for inline completions (LSP 3.18).
// Method: "textDocument/inlineCompletion"
// Spec: https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_inlineCompletion
type InlineCompletionParams struct {
	TextDocument TextDocumentIdentifier  `json:"textDocument"`
	Position     Position                `json:"position"`
	Context      InlineCompletionContext `json:"context"`
}

// Position is a zero-based line/character position.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// InlineCompletionTriggerKind mirrors the LSP 3.18 enum.
//
//	1 = Invoked  – explicitly requested by the user
//	2 = Automatic – triggered implicitly while typing
type InlineCompletionTriggerKind int

const (
	TriggerKindInvoked   InlineCompletionTriggerKind = 1
	TriggerKindAutomatic InlineCompletionTriggerKind = 2
)

// InlineCompletionContext provides context for the inline completion request.
type InlineCompletionContext struct {
	TriggerKind            InlineCompletionTriggerKind `json:"triggerKind"`
	SelectedCompletionInfo *SelectedCompletionInfo     `json:"selectedCompletionInfo,omitempty"`
}

// SelectedCompletionInfo describes the currently selected item in the
// completion widget when the inline completion was triggered.
type SelectedCompletionInfo struct {
	Range Range  `json:"range"`
	Text  string `json:"text"`
}

// Range is a start/end position range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// InlineCompletionList is the server's response to an inline completion request.
// The spec also allows returning []InlineCompletionItem or null directly;
// wrapping in a list object is the canonical form and what editors expect.
type InlineCompletionList struct {
	Items []InlineCompletionItem `json:"items"`
}

// StringValue is the structured form of insertText used for snippets (LSP 3.18).
// Kind must be "snippet" when used.
type StringValue struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// InsertTextValue holds either a plain string or a StringValue (snippet).
// It marshals to a JSON string when Kind is empty, or to a StringValue object.
type InsertTextValue struct {
	// Plain is set for plain-text completions.
	Plain string
	// Snippet is set for snippet completions (kind = "snippet").
	Snippet *StringValue
}

func (v InsertTextValue) MarshalJSON() ([]byte, error) {
	if v.Snippet != nil {
		return json.Marshal(v.Snippet)
	}
	return json.Marshal(v.Plain)
}

// NewPlainInsertText returns an InsertTextValue for a plain-text completion.
func NewPlainInsertText(s string) InsertTextValue { return InsertTextValue{Plain: s} }

// InlineCompletionItem is a single inline completion suggestion (LSP 3.18).
type InlineCompletionItem struct {
	// InsertText is the text to insert. Either a plain string or a StringValue
	// (for snippets). Per LSP 3.18: string | StringValue.
	InsertText InsertTextValue `json:"insertText"`

	// FilterText is used by the client to filter the item against the current
	// word at the cursor. Optional.
	FilterText string `json:"filterText,omitempty"`

	// Range is the range in the document to replace. When omitted the client
	// inserts at the current cursor position. Optional.
	Range *Range `json:"range,omitempty"`

	// Command is executed after the item is accepted. Optional.
	Command *Command `json:"command,omitempty"`
}

// Command represents an LSP command to execute.
type Command struct {
	Title     string        `json:"title"`
	Command   string        `json:"command"`
	Arguments []interface{} `json:"arguments,omitempty"`
}
