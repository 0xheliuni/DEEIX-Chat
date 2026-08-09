package contentmoderation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	domaincm "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/contentmoderation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type coordinatorTestRepo struct {
	mu          sync.Mutex
	applyErr    error
	applyCalls  int
	events      []domaincm.Event
	stats       []repository.DailyStatIncrement
	latestHit   *domaincm.Event
	runState    string
	staleRunIDs []string
}

func (r *coordinatorTestRepo) CreateEvent(_ context.Context, event *domaincm.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event != nil {
		r.events = append(r.events, *event)
	}
	return nil
}

func (r *coordinatorTestRepo) GetEventByPublicID(context.Context, string) (*domaincm.Event, error) {
	return nil, nil
}

func (r *coordinatorTestRepo) GetLatestHitEventByRunID(context.Context, string) (*domaincm.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latestHit, nil
}

func (r *coordinatorTestRepo) ListEvents(context.Context, domaincm.EventListFilter) ([]domaincm.Event, int64, error) {
	return nil, 0, nil
}

func (r *coordinatorTestRepo) ClearExpiredContent(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (r *coordinatorTestRepo) ClearExpiredContentByPublicIDs(context.Context, []string) (int64, error) {
	return 0, nil
}

func (r *coordinatorTestRepo) ListExpiredContentEvents(context.Context, time.Time, int) ([]domaincm.Event, error) {
	return nil, nil
}

func (r *coordinatorTestRepo) DeleteExpiredMetadata(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (r *coordinatorTestRepo) IncrementDailyStat(_ context.Context, input repository.DailyStatIncrement) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats = append(r.stats, input)
	return nil
}

func (r *coordinatorTestRepo) ListDailyStats(context.Context, time.Time, time.Time) ([]domaincm.DailyStat, error) {
	return nil, nil
}

func (r *coordinatorTestRepo) DeleteDailyStatsBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (r *coordinatorTestRepo) UpdateRunModeration(_ context.Context, _ string, state string, _ string, _ string) error {
	r.mu.Lock()
	r.runState = state
	r.mu.Unlock()
	return nil
}

func (r *coordinatorTestRepo) UpdateMessageModeration(context.Context, uint, string, string, string) error {
	return nil
}

func (r *coordinatorTestRepo) ApplyRunBlock(context.Context, string, bool, string, string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applyCalls++
	return nil, r.applyErr
}

func (r *coordinatorTestRepo) GetRunModerationState(context.Context, string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runState, nil
}

func (r *coordinatorTestRepo) ListStaleModeratingRuns(context.Context, time.Time, int) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.staleRunIDs...), nil
}

func TestKnownHitRemainsBlockedWhenDurableApplyFails(t *testing.T) {
	repo := &coordinatorTestRepo{applyErr: errors.New("database unavailable")}
	service := NewService(nil, repo, "", nil)
	coord := newRunCoordinator(service, RunMeta{RunID: "run_known_hit"}, runtimeConfig{Timeout: time.Second})
	coord.blocked = true
	coord.blockInfo = BlockInfo{EventID: "cme_hit", Direction: domaincm.DirectionOutput, Categories: []string{"violence"}}

	var emitted string
	coord.SetLiveEmitter(func(eventType string, _ map[string]interface{}) {
		emitted = eventType
	})

	result := coord.AfterGeneration(context.Background(), "", nil)
	if result.Block == nil || result.State != domaincm.ModerationStateBlocked {
		t.Fatalf("known hit must remain blocked, got %#v", result)
	}
	if !result.TerminalEmitted || emitted != "moderation_blocked" {
		t.Fatalf("expected moderation_blocked terminal event, emitted=%q result=%#v", emitted, result)
	}
	if !service.hasPendingBlock("run_known_hit") {
		t.Fatal("failed durable apply must be registered for compensation")
	}
	repo.mu.Lock()
	repo.applyErr = nil
	repo.mu.Unlock()
	service.recoverPendingBlocks(context.Background())
	if service.hasPendingBlock("run_known_hit") {
		t.Fatal("successful compensation must remove the pending block")
	}
}

func TestLateHitRunsFullBlockCompensation(t *testing.T) {
	repo := &coordinatorTestRepo{}
	service := NewService(nil, repo, "", nil)
	coord := newRunCoordinator(service, RunMeta{RunID: "run_late_hit"}, runtimeConfig{})
	coord.pending = 1
	coord.outputEnqueued = true
	coord.settled = true

	var emitted string
	service.SetEventEmitter(func(_ string, eventType string, _ map[string]interface{}) {
		emitted = eventType
	})
	task := &moderationTask{Coord: coord, Direction: domaincm.DirectionOutput, Modality: domaincm.ModalityText}
	lateBlock := coord.onTaskResult(task, taskResult{Hit: true, EventID: "cme_late", Categories: []string{"violence"}})
	if lateBlock == nil {
		t.Fatal("late hit must request background block compensation")
	}
	service.handleLateBlock(coord.meta, *lateBlock)

	repo.mu.Lock()
	applyCalls := repo.applyCalls
	repo.mu.Unlock()
	if applyCalls != 1 {
		t.Fatalf("expected one durable block apply, got %d", applyCalls)
	}
	if emitted != "moderation_blocked" {
		t.Fatalf("expected recovery moderation_blocked event, got %q", emitted)
	}
}

func TestWorkerPrefetchDoesNotBypassQueueCapacity(t *testing.T) {
	repo := &coordinatorTestRepo{}
	service := NewService(nil, repo, "", nil)
	service.maxConcurrency = 1
	service.queueCapacity = 1
	service.activeWorkers = 1 // keep the worker waiting for a logical slot

	ctx, cancel := context.WithCancel(context.Background())
	service.wg.Add(1)
	go service.workerLoop(ctx)

	coord := newRunCoordinator(service, RunMeta{RunID: "run_queue"}, runtimeConfig{})
	first := &moderationTask{Coord: coord, Direction: domaincm.DirectionOutput, Modality: domaincm.ModalityText}
	if err := service.enqueue(first); err != nil {
		t.Fatalf("enqueue first task: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for len(service.taskQueue) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(service.taskQueue) != 0 {
		t.Fatal("worker did not prefetch first task")
	}

	second := &moderationTask{Direction: domaincm.DirectionOutput, Modality: domaincm.ModalityText}
	if err := service.enqueue(second); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("prefetched waiting task must still consume queue capacity, got %v", err)
	}
	cancel()
	service.wg.Wait()
}

func TestOutputImageLoadFailureIsAuditedAsFailedOpen(t *testing.T) {
	repo := &coordinatorTestRepo{}
	service := NewService(nil, repo, "", nil)
	cfg := runtimeConfig{
		Policy: Policy{OutputImageCategories: []string{"violence"}},
	}
	service.cachedConfig = &cfg
	service.cachedAt = time.Now()
	coord := newRunCoordinator(service, RunMeta{RunID: "run_image_load"}, cfg)

	coord.RecordOutputImageFailure("file_123", errors.New("object missing"))

	coord.mu.Lock()
	failedOpen := coord.failedOpen
	coord.mu.Unlock()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if !failedOpen {
		t.Fatal("missing output image must mark the run failed-open")
	}
	if len(repo.events) != 1 || repo.events[0].Result != domaincm.ResultFailedOpen {
		t.Fatalf("expected one failed-open audit event, got %#v", repo.events)
	}
	if len(repo.stats) != 1 || repo.stats[0].FailureCount != 1 {
		t.Fatalf("expected failed-open statistics, got %#v", repo.stats)
	}
}
