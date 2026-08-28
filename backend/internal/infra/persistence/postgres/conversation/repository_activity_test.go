package conversation

import (
	"context"
	"testing"
	"time"

	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"gorm.io/gorm"
)

func TestGetDailyActivityByUserAggregatesAndFilters(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	startDate := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	midDate := startDate.AddDate(0, 0, 1)
	endDate := startDate.AddDate(0, 0, 3)

	messages := []model.Message{
		// 当日聚合目标：2 条 user/assistant，token 求和。
		// 首条压在本地零点：日期表达式若漏掉时区修正（如 SQLite strftime 转 UTC），本条会被聚到前一天，此用例即回归守卫。
		{BaseModel: model.BaseModel{CreatedAt: startDate}, PublicID: "seed_msg_1", ConversationID: 1, UserID: 7, Role: "user", TokenUsage: 100},
		{BaseModel: model.BaseModel{CreatedAt: startDate.Add(2 * time.Hour)}, PublicID: "seed_msg_2", ConversationID: 1, UserID: 7, Role: "assistant", TokenUsage: 233},
		// 次日：system/tool 不计入。
		{BaseModel: model.BaseModel{CreatedAt: midDate}, PublicID: "seed_msg_3", ConversationID: 1, UserID: 7, Role: "system"},
		{BaseModel: model.BaseModel{CreatedAt: midDate}, PublicID: "seed_msg_4", ConversationID: 1, UserID: 7, Role: "tool"},
		{BaseModel: model.BaseModel{CreatedAt: midDate}, PublicID: "seed_msg_5", ConversationID: 1, UserID: 7, Role: "assistant", TokenUsage: 5},
		// 其他用户不计入。
		{BaseModel: model.BaseModel{CreatedAt: startDate}, PublicID: "seed_msg_6", ConversationID: 2, UserID: 8, Role: "user", TokenUsage: 999},
		// 软删除不计入。
		{BaseModel: model.BaseModel{CreatedAt: startDate, DeletedAt: gorm.DeletedAt{Time: startDate.Add(time.Hour), Valid: true}}, PublicID: "seed_msg_7", ConversationID: 1, UserID: 7, Role: "user", TokenUsage: 777},
		// 起始边界之前不计入。
		{BaseModel: model.BaseModel{CreatedAt: startDate.Add(-time.Second)}, PublicID: "seed_msg_8", ConversationID: 1, UserID: 7, Role: "user", TokenUsage: 888},
	}
	if err := db.Create(&messages).Error; err != nil {
		t.Fatalf("seed messages: %v", err)
	}

	items, err := repo.GetDailyActivityByUser(ctx, 7, startDate, endDate)
	if err != nil {
		t.Fatalf("GetDailyActivityByUser() error = %v", err)
	}
	want := []domainconversation.MessageDailyActivity{
		{Date: "2026-08-01", MessageCount: 2, TokenUsage: 333},
		{Date: "2026-08-02", MessageCount: 1, TokenUsage: 5},
	}
	if len(items) != len(want) {
		t.Fatalf("GetDailyActivityByUser() returned %d rows (%v), want %d", len(items), items, len(want))
	}
	for i, item := range items {
		if item != want[i] {
			t.Fatalf("row %d = %+v, want %+v", i, item, want[i])
		}
	}
}
