package contentmoderation

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	appadmin "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/admin"
	appcm "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/contentmoderation"
	domaincm "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/contentmoderation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

type eventDTO struct {
	PublicID        string    `json:"publicID"`
	UserID          uint      `json:"userID"`
	UserLabel       string    `json:"userLabel"`
	Username        string    `json:"username"`
	ConversationID  uint      `json:"conversationID"`
	RunID           string    `json:"runID"`
	MessagePublicID string    `json:"messagePublicID"`
	Direction       string    `json:"direction"`
	Modality        string    `json:"modality"`
	Model           string    `json:"model"`
	PolicyVersion   int64     `json:"policyVersion"`
	Result          string    `json:"result"`
	CategoriesJSON  string    `json:"categoriesJSON"`
	LatencyMS       int64     `json:"latencyMS"`
	ErrorCode       string    `json:"errorCode"`
	ErrorMessage    string    `json:"errorMessage"`
	ContentSummary  string    `json:"contentSummary"`
	CreatedAt       time.Time `json:"createdAt"`
}

type userLabelResolver interface {
	ResolveUserLabels(ctx context.Context, userIDs []uint) map[uint]appadmin.UserLabel
}

type dailyStatDTO struct {
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
}

func toEventDTO(item domaincm.Event, label appadmin.UserLabel) eventDTO {
	return eventDTO{
		PublicID:        item.PublicID,
		UserID:          item.UserID,
		UserLabel:       label.Label,
		Username:        label.Username,
		ConversationID:  item.ConversationID,
		RunID:           item.RunID,
		MessagePublicID: item.MessagePublicID,
		Direction:       item.Direction,
		Modality:        item.Modality,
		Model:           item.Model,
		PolicyVersion:   item.PolicyVersion,
		Result:          item.Result,
		CategoriesJSON:  item.CategoriesJSON,
		LatencyMS:       item.LatencyMS,
		ErrorCode:       item.ErrorCode,
		ErrorMessage:    item.ErrorMessage,
		ContentSummary:  item.ContentSummary,
		CreatedAt:       item.CreatedAt,
	}
}

func toDailyStatDTO(item domaincm.DailyStat) dailyStatDTO {
	return dailyStatDTO{
		StatDate:     item.StatDate,
		Direction:    item.Direction,
		Modality:     item.Modality,
		Result:       item.Result,
		Category:     item.Category,
		CheckCount:   item.CheckCount,
		ContentItems: item.ContentItems,
		HitCount:     item.HitCount,
		FailureCount: item.FailureCount,
		LatencySumMS: item.LatencySumMS,
		LatencyCount: item.LatencyCount,
	}
}

// Handler exposes admin content-moderation APIs.
type Handler struct {
	service           *appcm.Service
	userLabelResolver userLabelResolver
}

// NewHandler creates the HTTP handler.
func NewHandler(service *appcm.Service) *Handler {
	return &Handler{service: service}
}

// SetUserLabelResolver injects batch user-label resolution for event lists/details.
func (h *Handler) SetUserLabelResolver(resolver userLabelResolver) {
	h.userLabelResolver = resolver
}

func (h *Handler) resolveUserLabels(ctx context.Context, userIDs []uint) map[uint]appadmin.UserLabel {
	if h.userLabelResolver == nil {
		return map[uint]appadmin.UserLabel{}
	}
	return h.userLabelResolver.ResolveUserLabels(ctx, userIDs)
}

// GetConfig godoc
// @Summary Get content moderation config
// @Tags admin-content-moderation
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.SuccessDoc
// @Router /admin/content-moderation/config [get]
func (h *Handler) GetConfig(c *gin.Context) {
	cfg, err := h.service.GetConfig(c.Request.Context(), middleware.MustUserRole(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{
		"config":     cfg,
		"categories": appcm.CategoryCatalog(),
	})
}

// UpdateConfig godoc
// @Summary Update content moderation config
// @Tags admin-content-moderation
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /admin/content-moderation/config [put]
func (h *Handler) UpdateConfig(c *gin.Context) {
	var req appcm.UpdateConfigInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg, err := h.service.UpdateConfig(c.Request.Context(), middleware.MustUserRole(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"config": cfg})
}

// Probe godoc
// @Summary Probe content moderation service
// @Tags admin-content-moderation
// @Produce json
// @Security BearerAuth
// @Router /admin/content-moderation/probe [post]
func (h *Handler) Probe(c *gin.Context) {
	result, err := h.service.Probe(c.Request.Context(), middleware.MustUserRole(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, result)
}

// GetStats godoc
// @Summary Get content moderation daily stats
// @Tags admin-content-moderation
// @Produce json
// @Security BearerAuth
// @Router /admin/content-moderation/stats [get]
func (h *Handler) GetStats(c *gin.Context) {
	filter := appcm.StatsFilter{}
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			filter.From = &t
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			filter.To = &t
		}
	}
	items, err := h.service.GetStats(c.Request.Context(), middleware.MustUserRole(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	out := make([]dailyStatDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toDailyStatDTO(item))
	}
	response.Success(c, gin.H{"items": out})
}

// ListEvents godoc
// @Summary List content moderation events
// @Tags admin-content-moderation
// @Produce json
// @Security BearerAuth
// @Router /admin/content-moderation/events [get]
func (h *Handler) ListEvents(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	userID, _ := strconv.ParseUint(c.Query("userId"), 10, 64)
	input := appcm.EventListInput{
		Direction: c.Query("direction"),
		Modality:  c.Query("modality"),
		Result:    c.Query("result"),
		Category:  c.Query("category"),
		UserID:    uint(userID),
		RunID:     c.Query("runId"),
		Page:      page,
		PageSize:  pageSize,
	}
	items, total, err := h.service.ListEvents(c.Request.Context(), middleware.MustUserRole(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	userIDs := make([]uint, 0, len(items))
	for _, item := range items {
		userIDs = append(userIDs, item.UserID)
	}
	userLabels := h.resolveUserLabels(c.Request.Context(), userIDs)
	out := make([]eventDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toEventDTO(item, userLabels[item.UserID]))
	}
	response.Success(c, gin.H{"items": out, "total": total, "page": page, "pageSize": pageSize})
}

// GetEvent godoc
// @Summary Get content moderation event detail
// @Tags admin-content-moderation
// @Produce json
// @Security BearerAuth
// @Router /admin/content-moderation/events/{eventID} [get]
func (h *Handler) GetEvent(c *gin.Context) {
	detail, err := h.service.GetEventDetail(
		c.Request.Context(),
		middleware.MustUserRole(c),
		c.Param("eventID"),
	)
	if err != nil {
		writeError(c, err)
		return
	}
	if detail != nil {
		label := h.resolveUserLabels(c.Request.Context(), []uint{detail.Event.UserID})[detail.Event.UserID]
		detail.UserLabel = label.Label
		detail.Username = label.Username
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, detail)
}

// GetEventImage godoc
// @Summary Stream a isolated moderation image
// @Tags admin-content-moderation
// @Produce octet-stream
// @Security BearerAuth
// @Router /admin/content-moderation/events/{eventID}/images/{index} [get]
func (h *Handler) GetEventImage(c *gin.Context) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		response.Error(c, http.StatusBadRequest, "invalid image index")
		return
	}
	data, mimeType, err := h.service.OpenEventImage(
		c.Request.Context(),
		middleware.MustUserRole(c),
		c.Param("eventID"),
		index,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, mimeType, data)
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, appcm.ErrSuperAdminRequired):
		response.Error(c, http.StatusForbidden, "superadmin permission required")
	case errors.Is(err, appcm.ErrAdminRequired):
		response.Error(c, http.StatusForbidden, "admin permission required")
	case errors.Is(err, appcm.ErrEventNotFound):
		response.Error(c, http.StatusNotFound, "content moderation event not found")
	case errors.Is(err, appcm.ErrServiceConfigRequired):
		response.ErrorWithCode(c, http.StatusBadRequest, "content_moderation.config_required", err.Error())
	case errors.Is(err, appcm.ErrInvalidBaseURL),
		errors.Is(err, appcm.ErrInvalidModel),
		errors.Is(err, appcm.ErrInvalidTimeout),
		errors.Is(err, appcm.ErrInvalidConcurrency),
		errors.Is(err, appcm.ErrInvalidQueueCapacity),
		errors.Is(err, appcm.ErrInvalidCategories),
		errors.Is(err, appcm.ErrImageTextOnlyCategory),
		errors.Is(err, appcm.ErrInvalidConfig):
		response.ErrorWithCode(c, http.StatusBadRequest, "content_moderation.invalid_config", err.Error())
	case errors.Is(err, appcm.ErrProbeFailed):
		response.ErrorWithCode(c, http.StatusBadRequest, "content_moderation.probe_failed", err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, "internal server error")
	}
}
