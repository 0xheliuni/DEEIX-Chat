package contentmoderation

import (
	"time"

	appcm "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/contentmoderation"
	domaincm "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/contentmoderation"
)

type policyDTO struct {
	InputTextCategories   []string `json:"inputTextCategories"`
	OutputTextCategories  []string `json:"outputTextCategories"`
	InputImageCategories  []string `json:"inputImageCategories"`
	OutputImageCategories []string `json:"outputImageCategories"`
	Version               int64    `json:"version"`
}

func toPolicyDTO(policy appcm.Policy) policyDTO {
	return policyDTO{
		InputTextCategories:   policy.InputTextCategories,
		OutputTextCategories:  policy.OutputTextCategories,
		InputImageCategories:  policy.InputImageCategories,
		OutputImageCategories: policy.OutputImageCategories,
		Version:               policy.Version,
	}
}

func (dto policyDTO) toApplication() appcm.Policy {
	return appcm.Policy{
		InputTextCategories:   dto.InputTextCategories,
		OutputTextCategories:  dto.OutputTextCategories,
		InputImageCategories:  dto.InputImageCategories,
		OutputImageCategories: dto.OutputImageCategories,
		Version:               dto.Version,
	}
}

type serviceConfigDTO struct {
	BaseURL        string    `json:"baseUrl"`
	APIKey         string    `json:"apiKey,omitempty"`
	APIKeyMasked   string    `json:"apiKeyMasked,omitempty"`
	HasAPIKey      bool      `json:"hasAPIKey"`
	Model          string    `json:"model"`
	TimeoutSeconds int       `json:"timeoutSeconds"`
	MaxConcurrency int       `json:"maxConcurrency"`
	QueueCapacity  int       `json:"queueCapacity"`
	Policy         policyDTO `json:"policy"`
	PolicyVersion  int64     `json:"policyVersion"`
}

func toServiceConfigDTO(config *appcm.ServiceConfig) *serviceConfigDTO {
	if config == nil {
		return nil
	}
	return &serviceConfigDTO{
		BaseURL:        config.BaseURL,
		APIKey:         config.APIKey,
		APIKeyMasked:   config.APIKeyMasked,
		HasAPIKey:      config.HasAPIKey,
		Model:          config.Model,
		TimeoutSeconds: config.TimeoutSeconds,
		MaxConcurrency: config.MaxConcurrency,
		QueueCapacity:  config.QueueCapacity,
		Policy:         toPolicyDTO(config.Policy),
		PolicyVersion:  config.PolicyVersion,
	}
}

type updateConfigRequest struct {
	BaseURL        *string    `json:"baseUrl"`
	APIKey         *string    `json:"apiKey"`
	ClearAPIKey    bool       `json:"clearAPIKey"`
	Model          *string    `json:"model"`
	TimeoutSeconds *int       `json:"timeoutSeconds"`
	MaxConcurrency *int       `json:"maxConcurrency"`
	QueueCapacity  *int       `json:"queueCapacity"`
	Policy         *policyDTO `json:"policy"`
}

func (request updateConfigRequest) toApplication() appcm.UpdateConfigInput {
	input := appcm.UpdateConfigInput{
		BaseURL:        request.BaseURL,
		APIKey:         request.APIKey,
		ClearAPIKey:    request.ClearAPIKey,
		Model:          request.Model,
		TimeoutSeconds: request.TimeoutSeconds,
		MaxConcurrency: request.MaxConcurrency,
		QueueCapacity:  request.QueueCapacity,
	}
	if request.Policy != nil {
		policy := request.Policy.toApplication()
		input.Policy = &policy
	}
	return input
}

type probeResultDTO struct {
	Valid   bool   `json:"valid"`
	Model   string `json:"model,omitempty"`
	Latency int64  `json:"latencyMS"`
	Error   string `json:"error,omitempty"`
}

type probeResponseDTO struct {
	Text  probeResultDTO `json:"text"`
	Image probeResultDTO `json:"image"`
}

func toProbeResponseDTO(result *appcm.ProbeResponse) *probeResponseDTO {
	if result == nil {
		return nil
	}
	return &probeResponseDTO{
		Text: probeResultDTO{
			Valid:   result.Text.Valid,
			Model:   result.Text.Model,
			Latency: result.Text.Latency,
			Error:   result.Text.Error,
		},
		Image: probeResultDTO{
			Valid:   result.Image.Valid,
			Model:   result.Image.Model,
			Latency: result.Image.Latency,
			Error:   result.Image.Error,
		},
	}
}

type eventRecordDTO struct {
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
	ImageCount          int       `json:"imageCount"`
	ImageMetaJSON       string    `json:"imageMetaJSON"`
	ContentExpiresAt    time.Time `json:"contentExpiresAt"`
	MetadataExpiresAt   time.Time `json:"metadataExpiresAt"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

func toEventRecordDTO(event domaincm.Event) eventRecordDTO {
	return eventRecordDTO{
		ID:                  event.ID,
		PublicID:            event.PublicID,
		UserID:              event.UserID,
		ConversationID:      event.ConversationID,
		RunID:               event.RunID,
		MessageID:           event.MessageID,
		MessagePublicID:     event.MessagePublicID,
		Direction:           event.Direction,
		Modality:            event.Modality,
		Model:               event.Model,
		PolicyVersion:       event.PolicyVersion,
		Result:              event.Result,
		CategoriesJSON:      event.CategoriesJSON,
		CategoryScoresJSON:  event.CategoryScoresJSON,
		LatencyMS:           event.LatencyMS,
		ErrorCode:           event.ErrorCode,
		ErrorMessage:        event.ErrorMessage,
		ContentLocationJSON: event.ContentLocationJSON,
		ContentSummary:      event.ContentSummary,
		ImageCount:          event.ImageCount,
		ImageMetaJSON:       event.ImageMetaJSON,
		ContentExpiresAt:    event.ContentExpiresAt,
		MetadataExpiresAt:   event.MetadataExpiresAt,
		CreatedAt:           event.CreatedAt,
		UpdatedAt:           event.UpdatedAt,
	}
}

type isolatedImageDTO struct {
	Index        int    `json:"index"`
	SHA256       string `json:"sha256"`
	MimeType     string `json:"mimeType"`
	SizeBytes    int64  `json:"sizeBytes"`
	StoragePath  string `json:"storagePath"`
	SourceFileID string `json:"sourceFileID,omitempty"`
}

func toIsolatedImageDTO(image domaincm.IsolatedImageMeta) isolatedImageDTO {
	return isolatedImageDTO{
		Index:        image.Index,
		SHA256:       image.SHA256,
		MimeType:     image.MimeType,
		SizeBytes:    image.SizeBytes,
		StoragePath:  image.StoragePath,
		SourceFileID: image.SourceFileID,
	}
}

type eventDetailDTO struct {
	Event           eventRecordDTO     `json:"event"`
	UserLabel       string             `json:"userLabel,omitempty"`
	Username        string             `json:"username,omitempty"`
	Categories      []string           `json:"categories"`
	CategoryScores  map[string]float64 `json:"categoryScores"`
	DecryptedText   string             `json:"decryptedText,omitempty"`
	TextAvailable   bool               `json:"textAvailable"`
	ImagesAvailable bool               `json:"imagesAvailable"`
	Images          []isolatedImageDTO `json:"images"`
}

func toEventDetailDTO(detail *appcm.EventDetail, userLabel string, username string) *eventDetailDTO {
	if detail == nil {
		return nil
	}
	images := make([]isolatedImageDTO, 0, len(detail.Images))
	for _, image := range detail.Images {
		images = append(images, toIsolatedImageDTO(image))
	}
	return &eventDetailDTO{
		Event:           toEventRecordDTO(detail.Event),
		UserLabel:       userLabel,
		Username:        username,
		Categories:      detail.Categories,
		CategoryScores:  detail.CategoryScores,
		DecryptedText:   detail.DecryptedText,
		TextAvailable:   detail.TextAvailable,
		ImagesAvailable: detail.ImagesAvailable,
		Images:          images,
	}
}
