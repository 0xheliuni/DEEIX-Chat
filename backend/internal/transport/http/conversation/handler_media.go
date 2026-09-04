package conversation

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"

	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// StreamImageGeneration 处理会话内图片生成流式状态接口。
func (h *Handler) StreamImageGeneration(c *gin.Context) {
	h.streamMediaImage(c, appconversation.MediaImageTaskGeneration)
}

// StreamImageEdit 处理会话内图片编辑流式状态接口。
func (h *Handler) StreamImageEdit(c *gin.Context) {
	h.streamMediaImage(c, appconversation.MediaImageTaskEdit)
}

// StreamVideoGeneration 处理会话内视频生成流式状态接口。
func (h *Handler) StreamVideoGeneration(c *gin.Context) {
	h.streamMediaVideo(c, appconversation.MediaVideoTaskGeneration)
}

// StreamVideoExtension 处理会话内视频扩展流式状态接口。
// @Summary 扩展会话视频
// @Tags Conversations
// @Accept json
// @Produce application/x-ndjson
// @Param id path string true "会话 Public ID"
// @Param payload body MediaVideoExtensionRequest true "视频扩展请求"
// @Success 200 {string} string "NDJSON stream"
// @Failure 400 {object} response.Envelope
// @Failure 401 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /conversations/{id}/media/videos/extensions/stream [post]
func (h *Handler) StreamVideoExtension(c *gin.Context) {
	h.streamMediaVideo(c, appconversation.MediaVideoTaskExtension)
}

type mediaVideoTransportRequest struct {
	Prompt                string
	Model                 string
	Options               map[string]interface{}
	ClientRunID           string
	FileIDs               []string
	ParentMessagePublicID string
	SourceMessagePublicID string
	BranchReason          string
}

// streamMediaVideo 统一视频生成与扩展的 HTTP、授权和事件转发流程。
func (h *Handler) streamMediaVideo(c *gin.Context, taskType appconversation.MediaVideoTaskType) {
	userID := middleware.MustUserID(c)
	publicID, err := stringParam(c, "id")
	if err != nil {
		response.ErrorFrom(c, http.StatusBadRequest, errInvalidConversationID)
		return
	}
	conversation, err := h.service.GetConversationByPublicID(c.Request.Context(), userID, publicID)
	if err != nil {
		if errors.Is(err, appconversation.ErrConversationNotFound) {
			response.ErrorFrom(c, http.StatusNotFound, err)
			return
		}
		response.InternalError(c)
		return
	}
	var req mediaVideoTransportRequest
	if taskType == appconversation.MediaVideoTaskExtension {
		var payload MediaVideoExtensionRequest
		if err := c.ShouldBindJSON(&payload); err != nil {
			response.InvalidRequestBody(c, err)
			return
		}
		req = mediaVideoTransportRequest{
			Prompt:                payload.Prompt,
			Model:                 payload.Model,
			Options:               payload.Options,
			ClientRunID:           payload.ClientRunID,
			FileIDs:               []string{payload.SourceVideoFileID},
			ParentMessagePublicID: payload.ParentMessagePublicID,
			SourceMessagePublicID: payload.SourceMessagePublicID,
			BranchReason:          payload.BranchReason,
		}
	} else {
		var payload MediaVideoRequest
		if err := c.ShouldBindJSON(&payload); err != nil {
			response.InvalidRequestBody(c, err)
			return
		}
		req = mediaVideoTransportRequest{
			Prompt:                payload.Prompt,
			Model:                 payload.Model,
			Options:               payload.Options,
			ClientRunID:           payload.ClientRunID,
			FileIDs:               payload.FileIDs,
			ParentMessagePublicID: payload.ParentMessagePublicID,
			SourceMessagePublicID: payload.SourceMessagePublicID,
			BranchReason:          payload.BranchReason,
		}
	}
	req.ClientRunID = appconversation.EnsureMessageGenerationRunID(req.ClientRunID)
	req.Options = sanitizeMessageOptions(req.Options)
	session, ok := h.beginUsageSession(c, mediaVideoBillingInput(userID, conversation, &req))
	if !ok {
		return
	}
	defer session.Close()

	h.streamMediaTask(
		c,
		req.ClientRunID,
		session,
		func(onEvent func(string, map[string]interface{}) error) (*appconversation.SendMessageResult, error) {
			return h.service.StreamMediaVideo(c.Request.Context(), appconversation.MediaVideoInput{
				UserID:                userID,
				ConversationID:        conversation.ID,
				RequestID:             middleware.MustRequestID(c),
				TaskType:              taskType,
				Prompt:                req.Prompt,
				PlatformModelName:     req.Model,
				Options:               req.Options,
				ClientRunID:           req.ClientRunID,
				FileIDs:               req.FileIDs,
				ParentMessagePublicID: req.ParentMessagePublicID,
				SourceMessagePublicID: req.SourceMessagePublicID,
				BranchReason:          req.BranchReason,
				UsageAuthorization:    session.Authorization(),
				OnEvent:               onEvent,
			})
		},
	)
}

// streamMediaImage 只负责 HTTP 绑定、计费预算预留和 NDJSON 事件转发，图片业务由 application 执行。
func (h *Handler) streamMediaImage(c *gin.Context, taskType appconversation.MediaImageTaskType) {
	userID := middleware.MustUserID(c)
	publicID, err := stringParam(c, "id")
	if err != nil {
		response.ErrorFrom(c, http.StatusBadRequest, errInvalidConversationID)
		return
	}
	conversation, err := h.service.GetConversationByPublicID(c.Request.Context(), userID, publicID)
	if err != nil {
		if errors.Is(err, appconversation.ErrConversationNotFound) {
			response.ErrorFrom(c, http.StatusNotFound, err)
			return
		}
		response.InternalError(c)
		return
	}
	var req MediaImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	req.ClientRunID = appconversation.EnsureMessageGenerationRunID(req.ClientRunID)
	req.Options = sanitizeMessageOptions(req.Options)
	session, ok := h.beginUsageSession(c, mediaImageBillingInput(userID, conversation, &req))
	if !ok {
		return
	}
	defer session.Close()

	h.streamMediaTask(
		c,
		req.ClientRunID,
		session,
		func(onEvent func(string, map[string]interface{}) error) (*appconversation.SendMessageResult, error) {
			return h.service.StreamMediaImage(c.Request.Context(), appconversation.MediaImageInput{
				UserID:                userID,
				ConversationID:        conversation.ID,
				RequestID:             middleware.MustRequestID(c),
				TaskType:              taskType,
				Prompt:                req.Prompt,
				PlatformModelName:     req.Model,
				Options:               req.Options,
				ClientRunID:           req.ClientRunID,
				FileIDs:               req.FileIDs,
				MaskFileID:            req.MaskFileID,
				ParentMessagePublicID: req.ParentMessagePublicID,
				SourceMessagePublicID: req.SourceMessagePublicID,
				BranchReason:          req.BranchReason,
				UsageAuthorization:    session.Authorization(),
				OnEvent:               onEvent,
			})
		},
	)
}

// streamMediaTask 统一媒体任务的 NDJSON 事件转发与计费收口：运行结束后由 session 结算或释放预算。
func (h *Handler) streamMediaTask(
	c *gin.Context,
	clientRunID string,
	session *appconversation.UsageSession,
	run func(onEvent func(string, map[string]interface{}) error) (*appconversation.SendMessageResult, error),
) {
	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	var clientDisconnected atomic.Bool
	flushStreamEvent := func(payload map[string]interface{}) error {
		payload = h.service.PublishMessageGenerationEvent(clientRunID, payload)
		if clientDisconnected.Load() {
			return nil
		}
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		if _, writeErr := c.Writer.Write(append(encoded, '\n')); writeErr != nil {
			clientDisconnected.Store(true)
			return writeErr
		}
		c.Writer.Flush()
		return nil
	}

	result, err := run(func(eventType string, payload map[string]interface{}) error {
		_ = flushStreamEvent(normalizeStreamEventPayload(eventType, payload))
		return nil
	})
	defer h.service.FinishMessageGeneration(clientRunID)

	if err == nil && result != nil && result.IsModerationBlocked() {
		if !result.ModerationTerminalEmitted() {
			_ = flushStreamEvent(moderationBlockedStreamPayload(result, session.Authorization()))
		}
		// 终态事件已发出，结算/释放失败由应用层记日志并标记对账，不能再向流推送第二个终态事件。
		_ = session.Finish(c.Request.Context(), result)
		return
	}
	if billingErr := session.Finish(c.Request.Context(), result); billingErr != nil {
		_ = flushStreamEvent(streamErrorPayloadWithResult(billingErr, result))
		return
	}
	if err != nil {
		_ = flushStreamEvent(streamErrorPayloadWithResult(err, result))
		return
	}
	if result == nil {
		return
	}
	if result.AssistantMessage.Status == "canceled" {
		_ = flushStreamEvent(streamErrorPayloadWithResult(appconversation.ErrMessageGenerationCanceled, result))
		return
	}
	_ = flushStreamEvent(map[string]interface{}{
		"type": "completed",
		"data": toSendMessageResponse(result),
	})
}

// mediaImageBillingInput 构造媒体任务复用消息计费链路所需的请求级上下文；运行结果由 UsageSession.Finish 补入。
func mediaImageBillingInput(
	userID uint,
	conversation *model.Conversation,
	req *MediaImageRequest,
) appconversation.SendMessageBillingInput {
	input := appconversation.SendMessageBillingInput{
		UserID:            userID,
		PlatformModelName: strings.TrimSpace(req.Model),
		ClientRunID:       strings.TrimSpace(req.ClientRunID),
	}
	if conversation != nil {
		input.ConversationID = conversation.ID
		input.ConversationModel = conversation.Model
		input.Conversation = conversation
	}
	return input
}

// mediaVideoBillingInput 构造视频任务复用消息计费链路所需的请求级上下文；运行结果由 UsageSession.Finish 补入。
func mediaVideoBillingInput(
	userID uint,
	conversation *model.Conversation,
	req *mediaVideoTransportRequest,
) appconversation.SendMessageBillingInput {
	input := appconversation.SendMessageBillingInput{
		UserID:            userID,
		PlatformModelName: strings.TrimSpace(req.Model),
		ClientRunID:       strings.TrimSpace(req.ClientRunID),
	}
	if conversation != nil {
		input.ConversationID = conversation.ID
		input.ConversationModel = conversation.Model
		input.Conversation = conversation
	}
	return input
}
