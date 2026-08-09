package contentmoderation

import "time"

// Direction indicates whether content is user input or model output.
const (
	DirectionInput  = "input"
	DirectionOutput = "output"
)

// Modality indicates text or image content.
const (
	ModalityText  = "text"
	ModalityImage = "image"
)

// Result values for events and daily stats.
const (
	ResultHit        = "hit"
	ResultFailedOpen = "failed_open"
	ResultPassed     = "passed"
)

// Run moderation_state values.
const (
	ModerationStateNotRequired = "not_required"
	ModerationStatePending     = "pending"
	ModerationStateModerating  = "moderating"
	ModerationStatePassed      = "passed"
	ModerationStateBlocked     = "blocked"
	ModerationStateFailedOpen  = "failed_open"
)

// Message/run status for blocked rounds.
const (
	StatusBlocked = "blocked"
)

// Error codes stored on failure events.
const (
	ErrorCodeTimeout       = "timeout"
	ErrorCodeRateLimited   = "rate_limited"
	ErrorCodeQueueFull     = "queue_full"
	ErrorCodeServiceError  = "service_error"
	ErrorCodeInvalidResp   = "invalid_response"
	ErrorCodeWorkerLost    = "worker_lost"
	ErrorCodeNetworkError  = "network_error"
	ErrorCodeConfigMissing = "config_missing"
)

// Event is a moderation check record (pass, hit, or failed-open).
type Event struct {
	ID                  uint      `json:"id"`
	PublicID            string    `json:"publicID"`
	UserID              uint      `json:"userID"`
	ConversationID      uint      `json:"conversationID"`
	RunID               string    `json:"runID"`
	MessageID           uint      `json:"messageID"`
	MessagePublicID     string    `json:"messagePublicID"`
	Direction           string    `json:"direction"`
	Modality            string    `json:"modality"`
	Model               string    `json:"model"`
	PolicyVersion       int64     `json:"policyVersion"`
	Result              string    `json:"result"`
	CategoriesJSON      string    `json:"categoriesJSON"`
	CategoryScoresJSON  string    `json:"categoryScoresJSON,omitempty"`
	LatencyMS           int64     `json:"latencyMS"`
	ErrorCode           string    `json:"errorCode"`
	ErrorMessage        string    `json:"errorMessage"`
	ContentLocationJSON string    `json:"contentLocationJSON"`
	ContentSummary      string    `json:"contentSummary"`
	EncryptedText       string    `json:"-"`
	ImageCount          int       `json:"imageCount"`
	ImageMetaJSON       string    `json:"imageMetaJSON"`
	ContentExpiresAt    time.Time `json:"contentExpiresAt"`
	MetadataExpiresAt   time.Time `json:"metadataExpiresAt"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// DailyStat aggregates anonymous counters for a calendar day.
type DailyStat struct {
	ID           uint      `json:"id"`
	StatDate     time.Time `json:"statDate"`
	Direction    string    `json:"direction"`
	Modality     string    `json:"modality"`
	Result       string    `json:"result"`
	Category     string    `json:"category"`
	CheckCount   int64     `json:"checkCount"`
	ContentItems int64     `json:"contentItems"`
	HitCount     int64     `json:"hitCount"`
	FailureCount int64     `json:"failureCount"`
	LatencySumMS int64     `json:"latencySumMS"`
	LatencyCount int64     `json:"latencyCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// EventListFilter filters super-admin event queries.
type EventListFilter struct {
	Direction string
	Modality  string
	Result    string
	Category  string
	UserID    uint
	RunID     string
	From      *time.Time
	To        *time.Time
	Offset    int
	Limit     int
}

// IsolatedImageMeta describes one encrypted image copy held for review.
type IsolatedImageMeta struct {
	Index        int    `json:"index"`
	SHA256       string `json:"sha256"`
	MimeType     string `json:"mimeType"`
	SizeBytes    int64  `json:"sizeBytes"`
	StoragePath  string `json:"storagePath"`
	SourceFileID string `json:"sourceFileID,omitempty"`
}

// ContentLocation describes where moderated content originated.
type ContentLocation struct {
	Field      string `json:"field,omitempty"`
	FileID     string `json:"fileID,omitempty"`
	Attachment int    `json:"attachment,omitempty"`
	ChunkIndex int    `json:"chunkIndex,omitempty"`
	ChunkCount int    `json:"chunkCount,omitempty"`
}
