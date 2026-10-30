package plugin

import "encoding/json"

// Error codes a plugin answers with.
const (
	CodeOther       = -32000 // any other failure: an ordinary track error
	CodeUnavailable = -32001 // the site is down or refuses: try another downloader
	CodeNeedsAction = -32002 // the user must act; Data is a NeedsAction
)

// Error is a JSON-RPC error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// NeedsAction is the Data of CodeNeedsAction: a text and the settings button to show.
type NeedsAction struct {
	Message string `json:"message"`
	Action  string `json:"action"`
}

// InitParams go with initialize and, whole, with every $/settings note.
type InitParams struct {
	Contract       int            `json:"contract"`
	Lang           string         `json:"lang"`
	Settings       map[string]any `json:"settings"`
	DataDir        string         `json:"dataDir"`
	Proxy          string         `json:"proxy"`          // the manual proxy as typed in Settings, "" for none
	EffectiveProxy string         `json:"effectiveProxy"` // what sonar would use for https://example.com, "" for direct
}

type Progress struct {
	Bytes  int64  `json:"bytes"`
	Size   int64  `json:"size"`
	Status string `json:"status"`
}

// ProgressParams is the $/progress payload: the request id and its progress. $/cancel carries just {"id"}.
type ProgressParams struct {
	ID int64 `json:"id"`
	Progress
}

type PluginStatus struct {
	Available   bool         `json:"available"`
	NeedsAction *NeedsAction `json:"needsAction,omitempty"`
	Reason      string       `json:"reason,omitempty"`
}

// Methods. Notifications start with "$/".
const (
	MInitialize      = "initialize"
	MCancel          = "$/cancel"
	MProgress        = "$/progress"
	MStatus          = "$/status"
	MSettingsChanged = "$/settings"
)

// Message is one JSON-RPC 2.0 line: a request (ID and Method), a notification (Method only)
// or an answer (ID with Result or Error).
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}
