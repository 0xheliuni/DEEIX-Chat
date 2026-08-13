package conversation

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	appusersettings "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/usersettings"
	domainusersettings "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/usersettings"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type mutableUserSettingsRepository struct {
	repository.ConversationRepository
	mu        sync.RWMutex
	values    map[uint]map[string]string
	beforeGet func()
}

func (r *mutableUserSettingsRepository) GetUserSettingValue(_ context.Context, userID uint, key string) (string, error) {
	r.mu.RLock()
	value := r.values[userID][key]
	r.mu.RUnlock()
	if r.beforeGet != nil {
		r.beforeGet()
	}
	return value, nil
}

func (r *mutableUserSettingsRepository) ListByUserID(_ context.Context, userID uint) ([]domainusersettings.UserSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := r.values[userID]
	items := make([]domainusersettings.UserSetting, 0, len(values))
	for key, value := range values {
		items = append(items, domainusersettings.UserSetting{UserID: userID, Key: key, Value: value})
	}
	return items, nil
}

func (r *mutableUserSettingsRepository) Upsert(_ context.Context, items []domainusersettings.UserSetting) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range items {
		if r.values[item.UserID] == nil {
			r.values[item.UserID] = make(map[string]string)
		}
		r.values[item.UserID][item.Key] = item.Value
	}
	return nil
}

func (r *mutableUserSettingsRepository) setValue(userID uint, key, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[userID][key] = value
}

type failingUpsertUserSettingsRepository struct {
	*mutableUserSettingsRepository
}

func (r *failingUpsertUserSettingsRepository) Upsert(_ context.Context, _ []domainusersettings.UserSetting) error {
	return errors.New("upsert failed")
}

func TestConversationSettingsReadPatchedValuesImmediately(t *testing.T) {
	const userID uint = 17
	ctx := context.Background()
	runtimeCfg := config.NewRuntime(config.Config{ContextCompactEnabled: true})
	repo := &mutableUserSettingsRepository{
		values: map[uint]map[string]string{
			userID: {
				"chat.reasoning_content_passback": "true",
				"chat.context_compact_auto":       "true",
				"chat.file_mode":                  "auto",
			},
		},
	}
	conversationService := &Service{
		cfg:  runtimeCfg,
		repo: repo,
	}
	settingsService := appusersettings.NewService(repo)
	settingsService.SetCacheInvalidator(conversationService.InvalidateUserSettingCache)

	// 初始读取走生产路径并填充内存缓存。
	if !conversationService.reasoningContentPassbackEnabled(ctx, userID, &channel.ResolvedRoute{ReasoningContentPassback: true}) {
		t.Fatal("expected initial reasoning passback to be enabled")
	}
	if !conversationService.resolveContextCompactionPolicy(ctx, runtimeCfg.Snapshot(), userID).EffectiveEnabled() {
		t.Fatal("expected initial context compaction to be enabled")
	}
	initialFileMode, err := conversationService.getUserSettingCached(ctx, userID, "chat.file_mode")
	if err != nil {
		t.Fatalf("initial file mode read: %v", err)
	}
	if initialFileMode != "auto" {
		t.Fatalf("initial message file mode = %q, want auto", initialFileMode)
	}

	for key, want := range map[string]string{
		"chat.reasoning_content_passback": "true",
		"chat.context_compact_auto":       "true",
		"chat.file_mode":                  "auto",
	} {
		if v, ok := conversationService.userSettingCache.Load(userSettingCacheKey(userID, key)); !ok {
			t.Fatalf("expected user setting cache entry for %q to be populated", key)
		} else if entry := v.(*cachedUserSetting); !entry.valid || entry.value != want {
			t.Fatalf("cached user setting %q = %q (valid=%v), want %q", key, entry.value, entry.valid, want)
		}
	}

	if _, err := settingsService.PatchSettings(ctx, userID, map[string]string{
		"chat.reasoning_content_passback": "false",
		"chat.context_compact_auto":       "false",
		"chat.file_mode":                  "rag",
	}); err != nil {
		t.Fatalf("patch settings: %v", err)
	}

	// 写入成功后，生产失效回调应清除相关缓存条目。
	for _, key := range []string{
		"chat.reasoning_content_passback",
		"chat.context_compact_auto",
		"chat.file_mode",
	} {
		if _, ok := conversationService.userSettingCache.Load(userSettingCacheKey(userID, key)); ok {
			t.Fatalf("expected user setting cache entry for %q to be invalidated after patch", key)
		}
	}

	if conversationService.reasoningContentPassbackEnabled(ctx, userID, &channel.ResolvedRoute{ReasoningContentPassback: true}) {
		t.Fatal("expected patched reasoning passback to be disabled")
	}
	if conversationService.resolveContextCompactionPolicy(ctx, runtimeCfg.Snapshot(), userID).EffectiveEnabled() {
		t.Fatal("expected patched context compaction to be disabled")
	}
	updatedFileMode, err := conversationService.getUserSettingCached(ctx, userID, "chat.file_mode")
	if err != nil {
		t.Fatalf("updated file mode read: %v", err)
	}
	if updatedFileMode != "rag" {
		t.Fatalf("updated message file mode = %q, want rag", updatedFileMode)
	}
}

func TestConversationSettingsCacheDoesNotRepopulateAfterInvalidation(t *testing.T) {
	const userID uint = 19
	ctx := context.Background()
	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	var readOnce sync.Once
	repo := &mutableUserSettingsRepository{
		values: map[uint]map[string]string{
			userID: {"chat.file_mode": "auto"},
		},
		beforeGet: func() {
			readOnce.Do(func() {
				close(readStarted)
				<-releaseRead
			})
		},
	}
	conversationService := &Service{repo: repo}
	settingsService := appusersettings.NewService(repo)
	settingsService.SetCacheInvalidator(conversationService.InvalidateUserSettingCache)

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		if value, err := conversationService.getUserSettingCached(ctx, userID, "chat.file_mode"); err != nil || value != "auto" {
			t.Errorf("initial concurrent read = %q (err %v), want auto", value, err)
		}
	}()

	<-readStarted
	if _, err := settingsService.PatchSettings(ctx, userID, map[string]string{"chat.file_mode": "rag"}); err != nil {
		t.Fatalf("patch settings: %v", err)
	}
	if _, ok := conversationService.userSettingCache.Load(userSettingCacheKey(userID, "chat.file_mode")); ok {
		t.Fatal("expected cache entry to remain absent while stale read is blocked")
	}

	close(releaseRead)
	<-readDone
	if _, ok := conversationService.userSettingCache.Load(userSettingCacheKey(userID, "chat.file_mode")); ok {
		t.Fatal("stale read must not repopulate cache after invalidation")
	}

	value, err := conversationService.getUserSettingCached(ctx, userID, "chat.file_mode")
	if err != nil {
		t.Fatalf("updated setting read: %v", err)
	}
	if value != "rag" {
		t.Fatalf("updated file mode = %q, want rag", value)
	}
}

func TestConversationSettingsCacheSurvivesFailedUpsert(t *testing.T) {
	const userID uint = 18
	ctx := context.Background()
	base := &mutableUserSettingsRepository{
		values: map[uint]map[string]string{
			userID: {
				"chat.file_mode": "auto",
			},
		},
	}
	repo := &failingUpsertUserSettingsRepository{mutableUserSettingsRepository: base}
	conversationService := &Service{repo: repo}
	settingsService := appusersettings.NewService(repo)
	settingsService.SetCacheInvalidator(conversationService.InvalidateUserSettingCache)

	if got, err := conversationService.getUserSettingCached(ctx, userID, "chat.file_mode"); err != nil || got != "auto" {
		t.Fatalf("initial cached file mode = %q (err %v), want auto", got, err)
	}

	// 模拟外部 DB 写入；若缓存被误失效则下一次读取会拿到新值。
	base.setValue(userID, "chat.file_mode", "rag")

	if _, err := settingsService.PatchSettings(ctx, userID, map[string]string{
		"chat.file_mode": "full_context",
	}); err == nil {
		t.Fatal("expected patch settings to fail")
	}

	if got, err := conversationService.getUserSettingCached(ctx, userID, "chat.file_mode"); err != nil || got != "auto" {
		t.Fatalf("cached file mode after failed upsert = %q (err %v), want auto from cache", got, err)
	}
}
