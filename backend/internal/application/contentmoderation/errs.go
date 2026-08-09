package contentmoderation

import "errors"

var (
	ErrSuperAdminRequired    = errors.New("superadmin permission required")
	ErrAdminRequired         = errors.New("admin permission required")
	ErrInvalidConfig         = errors.New("invalid content moderation config")
	ErrServiceConfigRequired = errors.New("content moderation service config is required when policies are enabled")
	ErrInvalidBaseURL        = errors.New("invalid content moderation base url")
	ErrInvalidModel          = errors.New("invalid content moderation model")
	ErrInvalidTimeout        = errors.New("content moderation timeout must be between 1 and 60 seconds")
	ErrInvalidConcurrency    = errors.New("content moderation max concurrency must be between 1 and 64")
	ErrInvalidQueueCapacity  = errors.New("content moderation queue capacity must be between 1 and 4096")
	ErrInvalidCategories     = errors.New("invalid content moderation categories")
	ErrImageTextOnlyCategory = errors.New("text-only categories cannot be selected for image policies")
	ErrEventNotFound         = errors.New("content moderation event not found")
	ErrProbeFailed           = errors.New("content moderation probe failed")
	ErrQueueFull             = errors.New("content moderation queue is full")
	ErrModerationTimeout     = errors.New("content moderation timed out")
	ErrModerationService     = errors.New("content moderation service error")
	ErrModerationRateLimited = errors.New("content moderation rate limited")
	ErrModerationInvalidResp = errors.New("content moderation invalid response")
	ErrModerationNetwork     = errors.New("content moderation network error")
	ErrWorkerLost            = errors.New("content moderation worker lost")
)
