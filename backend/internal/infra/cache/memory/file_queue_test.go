package memory

import (
	"context"
	"testing"
	"time"
)

func TestFileProcessingLeaseTransfersOnlyAfterTimeout(t *testing.T) {
	cache := New()
	ctx := context.Background()
	if err := cache.EnqueueFileProcessing(ctx, 7, "file_1", 0, ""); err != nil {
		t.Fatalf("enqueue file: %v", err)
	}
	messages, err := cache.ReadFileProcessingMessages(ctx, "worker_a")
	if err != nil || len(messages) != 1 {
		t.Fatalf("read file message: messages=%#v err=%v", messages, err)
	}
	message := messages[0]
	if claimed, err := cache.ClaimTimedOutFileProcessingMessages(ctx, "worker_b"); err != nil || len(claimed) != 0 {
		t.Fatalf("active lease must not transfer: claimed=%#v err=%v", claimed, err)
	}

	cache.mu.Lock()
	lease := cache.fileInflight[message.ID]
	lease.leasedAt = time.Now().Add(-fileProcessingMinIdle - time.Second)
	cache.fileInflight[message.ID] = lease
	cache.mu.Unlock()

	claimed, err := cache.ClaimTimedOutFileProcessingMessages(ctx, "worker_b")
	if err != nil || len(claimed) != 1 || !claimed[0].Reclaimed {
		t.Fatalf("claim expired lease: claimed=%#v err=%v", claimed, err)
	}
	if owned, err := cache.RenewFileProcessingMessageLease(ctx, "worker_a", message.ID); err != nil || owned {
		t.Fatalf("previous owner retained lease: owned=%v err=%v", owned, err)
	}
	if settled, err := cache.SettleFileProcessingMessage(ctx, "worker_a", message.ID); err != nil || settled {
		t.Fatalf("previous owner settled transferred message: settled=%v err=%v", settled, err)
	}
	if owned, err := cache.RenewFileProcessingMessageLease(ctx, "worker_b", message.ID); err != nil || !owned {
		t.Fatalf("new owner does not own lease: owned=%v err=%v", owned, err)
	}
	if settled, err := cache.SettleFileProcessingMessage(ctx, "worker_b", message.ID); err != nil || !settled {
		t.Fatalf("new owner failed to settle message: settled=%v err=%v", settled, err)
	}
}
