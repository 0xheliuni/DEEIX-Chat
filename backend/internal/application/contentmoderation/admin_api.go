package contentmoderation

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	domaincm "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/contentmoderation"
)

// StatsFilter bounds the admin stats query.
type StatsFilter struct {
	From *time.Time
	To   *time.Time
}

// EventListInput is the super-admin events query.
type EventListInput struct {
	Direction string
	Modality  string
	Result    string
	Category  string
	UserID    uint
	RunID     string
	From      *time.Time
	To        *time.Time
	Page      int
	PageSize  int
}

// EventDetail is the super-admin detail payload (may include decrypted text).
type EventDetail struct {
	Event           domaincm.Event               `json:"event"`
	UserLabel       string                       `json:"userLabel,omitempty"`
	Username        string                       `json:"username,omitempty"`
	Categories      []string                     `json:"categories"`
	CategoryScores  map[string]float64           `json:"categoryScores"`
	DecryptedText   string                       `json:"decryptedText,omitempty"`
	TextAvailable   bool                         `json:"textAvailable"`
	ImagesAvailable bool                         `json:"imagesAvailable"`
	Images          []domaincm.IsolatedImageMeta `json:"images"`
}

// GetStats returns anonymous aggregates for the last 90 days (admin+).
func (s *Service) GetStats(ctx context.Context, actorRole string, filter StatsFilter) ([]domaincm.DailyStat, error) {
	if !isAdminRole(actorRole) {
		return nil, ErrAdminRequired
	}
	now := time.Now().UTC()
	to := now
	from := now.Add(-metadataRetention)
	if filter.To != nil && !filter.To.IsZero() {
		to = filter.To.UTC()
	}
	if filter.From != nil && !filter.From.IsZero() {
		from = filter.From.UTC()
	}
	minFrom := now.Add(-metadataRetention)
	if from.Before(minFrom) {
		from = minFrom
	}
	if to.Before(from) {
		to = from
	}
	return s.repo.ListDailyStats(ctx, from, to)
}

// ListEvents lists hit/fail metadata for super-admin.
func (s *Service) ListEvents(ctx context.Context, actorRole string, input EventListInput) ([]domaincm.Event, int64, error) {
	if !isSuperAdmin(actorRole) {
		return nil, 0, ErrSuperAdminRequired
	}
	page := input.Page
	if page < 1 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return s.repo.ListEvents(ctx, domaincm.EventListFilter{
		Direction: strings.TrimSpace(input.Direction),
		Modality:  strings.TrimSpace(input.Modality),
		Result:    strings.TrimSpace(input.Result),
		Category:  strings.TrimSpace(input.Category),
		UserID:    input.UserID,
		RunID:     strings.TrimSpace(input.RunID),
		From:      input.From,
		To:        input.To,
		Offset:    (page - 1) * pageSize,
		Limit:     pageSize,
	})
}

// GetEventDetail returns decrypted text when still retained.
// Viewing moderation events is intentionally not written to the operation audit log;
// the dedicated moderation events log is the source of truth.
func (s *Service) GetEventDetail(
	ctx context.Context,
	actorRole string,
	eventID string,
) (*EventDetail, error) {
	if !isSuperAdmin(actorRole) {
		return nil, ErrSuperAdminRequired
	}
	event, err := s.repo.GetEventByPublicID(ctx, strings.TrimSpace(eventID))
	if err != nil || event == nil {
		return nil, ErrEventNotFound
	}
	detail := &EventDetail{Event: *event}
	_ = json.Unmarshal([]byte(event.CategoriesJSON), &detail.Categories)
	_ = json.Unmarshal([]byte(event.CategoryScoresJSON), &detail.CategoryScores)
	_ = json.Unmarshal([]byte(event.ImageMetaJSON), &detail.Images)

	if event.Result == domaincm.ResultHit &&
		event.Modality == domaincm.ModalityText &&
		strings.TrimSpace(event.EncryptedText) != "" &&
		time.Now().Before(event.ContentExpiresAt) {
		if plain, decErr := s.decryptText(event.EncryptedText); decErr == nil {
			detail.DecryptedText = plain
			detail.TextAvailable = true
		}
	}
	if event.Modality == domaincm.ModalityImage && len(detail.Images) > 0 && time.Now().Before(event.ContentExpiresAt) {
		detail.ImagesAvailable = true
	}
	return detail, nil
}

// OpenEventImage decrypts an isolated image for super-admin streaming.
// Image viewing is intentionally not written to the operation audit log.
func (s *Service) OpenEventImage(
	ctx context.Context,
	actorRole string,
	eventID string,
	index int,
) (data []byte, mimeType string, err error) {
	if !isSuperAdmin(actorRole) {
		return nil, "", ErrSuperAdminRequired
	}
	event, err := s.repo.GetEventByPublicID(ctx, strings.TrimSpace(eventID))
	if err != nil || event == nil {
		return nil, "", ErrEventNotFound
	}
	if time.Now().After(event.ContentExpiresAt) {
		return nil, "", ErrEventNotFound
	}
	var images []domaincm.IsolatedImageMeta
	_ = json.Unmarshal([]byte(event.ImageMetaJSON), &images)
	var meta *domaincm.IsolatedImageMeta
	for i := range images {
		if images[i].Index == index {
			meta = &images[i]
			break
		}
	}
	if meta == nil || s.objectStore == nil {
		return nil, "", ErrEventNotFound
	}
	raw, err := s.objectStore.Open(ctx, meta.StoragePath)
	if err != nil {
		return nil, "", err
	}
	// Isolated images are stored as encryptBytes payloads (v1: base64), not UTF-8 text.
	plain, err := s.decryptBytes(string(raw))
	if err != nil {
		return nil, "", err
	}
	return plain, firstNonEmpty(meta.MimeType, "image/png"), nil
}

// CategoryCatalog returns category lists for the admin UI.
func CategoryCatalog() map[string][]string {
	return map[string][]string{
		"text":  AllTextCategories(),
		"image": ImageCategories(),
	}
}
