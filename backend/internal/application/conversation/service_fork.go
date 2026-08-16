package conversation

import (
	"context"
	"errors"
	"strings"
	"time"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/google/uuid"
)

// forkAncestorMaxDepth 限制 fork 复制的祖先链长度，与仓储层递归 CTE 的上限对齐。
const forkAncestorMaxDepth = 2000

// ForkConversationFromMessage 将会话从开头到指定消息（含）的祖先链复制为一个新会话。
// 新会话不携带原会话的生成运行、处理轨迹、计费与压缩快照；附件以引用方式复用原文件
// 对象（同用户，不重复占用存储配额）。原会话保持不变。
func (s *Service) ForkConversationFromMessage(ctx context.Context, userID uint, conversationPublicID string, messagePublicID string) (*model.Conversation, error) {
	normalizedConversationID := strings.TrimSpace(conversationPublicID)
	if normalizedConversationID == "" {
		return nil, ErrConversationNotFound
	}
	normalizedMessageID := strings.TrimSpace(messagePublicID)
	if normalizedMessageID == "" {
		return nil, ErrMessageNotFound
	}

	conversation, err := s.repo.GetConversationByPublicID(ctx, normalizedConversationID, userID)
	if err != nil {
		return nil, ErrConversationNotFound
	}

	message, err := s.repo.GetMessageByPublicIDForUser(ctx, userID, normalizedMessageID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrMessageNotFound
		}
		return nil, err
	}
	if message.ConversationID != conversation.ID {
		return nil, ErrMessageNotFound
	}
	if message.Status == "pending" {
		return nil, ErrMessageForkStateInvalid
	}

	// 祖先链 CTE 按 id 升序返回根→目标消息的线性路径，父消息必然先于子消息，
	// 可按顺序逐条克隆并即时回填父 ID 映射。
	path, err := s.repo.ListMessageAncestors(ctx, conversation.ID, message.ID, forkAncestorMaxDepth)
	if err != nil {
		return nil, err
	}
	if len(path) == 0 {
		return nil, ErrMessageNotFound
	}

	target := &model.Conversation{
		UserID:                userID,
		ProjectID:             conversation.ProjectID,
		PublicID:              normalizePublicID(uuid.NewString()),
		Title:                 conversation.Title,
		LabelsJSON:            conversation.LabelsJSON,
		LabelsManuallyManaged: conversation.LabelsManuallyManaged,
		Model:                 conversation.Model,
		Provider:              conversation.Provider,
		SessionKey:            uuid.NewString(),
		MessageCount:          0,
		Status:                "active",
		ContextPolicy:         buildContextPolicyJSON(s.cfg.Snapshot()),
		LastCompactedAt:       nil,
		LastResponseID:        "",
	}
	if err = s.repo.CreateConversation(ctx, target); err != nil {
		return nil, err
	}

	clonedMessageIDs := make(map[string]uint, len(path))
	for _, sourceMessage := range path {
		cloned, err := s.cloneForkedMessage(ctx, userID, target.ID, sourceMessage, clonedMessageIDs)
		if err != nil {
			return nil, err
		}
		clonedMessageIDs[strings.TrimSpace(sourceMessage.PublicID)] = cloned.ID
		if err = s.cloneForkedMessageAttachments(ctx, userID, target.ID, cloned.ID, sourceMessage.Attachments); err != nil {
			return nil, err
		}
	}
	if err = s.repo.IncrementMessageCount(ctx, target.ID, len(path)); err != nil {
		return nil, err
	}
	target.MessageCount = len(path)
	return target, nil
}

func (s *Service) cloneForkedMessage(
	ctx context.Context,
	userID uint,
	conversationID uint,
	source model.Message,
	clonedMessageIDs map[string]uint,
) (*model.Message, error) {
	var parentMessageID *uint
	if parentID, ok := clonedMessageIDs[strings.TrimSpace(source.ParentPublicID)]; ok {
		value := parentID
		parentMessageID = &value
	}
	// 祖先链是线性路径：源消息的重试/编辑来源（SourceMessageID）不在路径内，置空即可。
	branchReason := strings.TrimSpace(source.BranchReason)
	if branchReason == "" {
		branchReason = "default"
	}
	contentType := strings.TrimSpace(source.ContentType)
	if contentType == "" {
		contentType = "text"
	}
	message := &model.Message{
		ConversationID:   conversationID,
		UserID:           userID,
		PublicID:         normalizePublicID(uuid.NewString()),
		ParentMessageID:  parentMessageID,
		Role:             strings.TrimSpace(source.Role),
		ContentType:      contentType,
		Content:          source.Content,
		ReasoningContent: source.ReasoningContent,
		BranchReason:     branchReason,
		TokenUsage:       source.TokenUsage,
		InputTokens:      source.InputTokens,
		OutputTokens:     source.OutputTokens,
		CacheReadTokens:  source.CacheReadTokens,
		CacheWriteTokens: source.CacheWriteTokens,
		ReasoningTokens:  source.ReasoningTokens,
		LatencyMS:        source.LatencyMS,
		BilledCurrency:   "USD",
		BilledNanousd:    0,
		PricingSnapshot:  "",
		Status:           normalizeForkedMessageStatus(source.Status),
		ErrorCode:        source.ErrorCode,
		ErrorMessage:     source.ErrorMessage,
		EditedAt:         source.EditedAt,
	}
	if message.Role == "" {
		message.Role = "assistant"
	}
	if err := s.repo.CreateMessage(ctx, message); err != nil {
		return nil, err
	}
	return message, nil
}

// normalizeForkedMessageStatus fork 不复制运行记录，pending 消息没有可续传的运行，
// 统一落到 interrupted 保持「可继续/可重试」的语义，避免界面停留在永久加载态。
func normalizeForkedMessageStatus(status string) string {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return "success"
	}
	if trimmed == "pending" {
		return "interrupted"
	}
	return trimmed
}

func (s *Service) cloneForkedMessageAttachments(
	ctx context.Context,
	userID uint,
	conversationID uint,
	messageID uint,
	rawAttachments string,
) error {
	snapshots := parseSharedAttachmentSnapshots(rawAttachments)
	if len(snapshots) == 0 {
		return nil
	}
	now := time.Now().UTC()
	items := make([]model.Attachment, 0, len(snapshots))
	for _, snapshot := range snapshots {
		fileID := strings.TrimSpace(snapshot.FileID)
		if fileID == "" {
			continue
		}
		// 同用户 fork：附件直接引用原文件对象，不复制存储、不重复占用配额；
		// 文件已被删除时跳过该附件，不阻断 fork。
		file, err := s.repo.GetActiveFileObjectByID(ctx, userID, fileID)
		if err != nil {
			if isFileNotFoundError(err) {
				continue
			}
			return err
		}
		kind := strings.TrimSpace(snapshot.Kind)
		if kind == "" {
			kind = "file"
		}
		fileName := strings.TrimSpace(snapshot.FileName)
		if fileName == "" {
			fileName = file.FileName
		}
		mimeType := strings.TrimSpace(snapshot.MimeType)
		if mimeType == "" {
			mimeType = file.MimeType
		}
		fileSize := snapshot.FileSize
		if fileSize <= 0 {
			fileSize = file.SizeBytes
		}
		items = append(items, model.Attachment{
			ConversationID: conversationID,
			MessageID:      messageID,
			UserID:         userID,
			FileID:         file.FileID,
			Kind:           kind,
			FileName:       fileName,
			MimeType:       mimeType,
			FileSize:       fileSize,
			SHA256:         file.SHA256,
			StoragePath:    file.StoragePath,
			Status:         "active",
			MetaJSON:       generatedVideoAttachmentMetaJSON(snapshot.DurationSeconds),
			UploadedAt:     now,
		})
	}
	if len(items) == 0 {
		return nil
	}
	return s.repo.CreateAttachments(ctx, items)
}
