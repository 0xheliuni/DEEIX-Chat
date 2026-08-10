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
	ID                  uint
	PublicID            string
	UserID              uint
	ConversationID      uint
	RunID               string
	MessageID           uint
	MessagePublicID     string
	Direction           string
	Modality            string
	Model               string
	PolicyVersion       int64
	Result              string
	CategoriesJSON      string
	CategoryScoresJSON  string
	LatencyMS           int64
	ErrorCode           string
	ErrorMessage        string
	ContentLocationJSON string
	ContentSummary      string
	EncryptedText       string
	ImageCount          int
	ImageMetaJSON       string
	ContentExpiresAt    time.Time
	MetadataExpiresAt   time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// DailyStat aggregates anonymous counters for a calendar day.
type DailyStat struct {
	ID           uint
	StatDate     time.Time
	Direction    string
	Modality     string
	Result       string
	Category     string
	CheckCount   int64
	ContentItems int64
	HitCount     int64
	FailureCount int64
	LatencySumMS int64
	LatencyCount int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
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
	Index        int
	SHA256       string
	MimeType     string
	SizeBytes    int64
	StoragePath  string
	SourceFileID string
}

// ContentLocation describes where moderated content originated.
type ContentLocation struct {
	Field      string
	FileID     string
	Attachment int
	ChunkIndex int
	ChunkCount int
}
