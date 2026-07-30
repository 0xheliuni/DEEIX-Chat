package conversation

import (
	"context"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

func TestCanFailoverMessageRoute(t *testing.T) {
	retryable := &llm.UpstreamError{StatusCode: 503, Message: "unavailable"}
	tests := []struct {
		name                 string
		attemptCount         int
		visibleDeltaCount    int
		attemptHadSideEffect bool
		cause                error
		want                 bool
	}{
		{name: "first route failed", attemptCount: 1, cause: retryable, want: true},
		{name: "second route failed", attemptCount: 2, cause: retryable, want: true},
		{name: "attempt limit reached", attemptCount: maxRequestRouteAttempts, cause: retryable, want: false},
		{name: "visible output emitted", attemptCount: 1, visibleDeltaCount: 1, cause: retryable, want: false},
		{name: "non-text side effect emitted", attemptCount: 1, attemptHadSideEffect: true, cause: retryable, want: false},
		{name: "request rejected", attemptCount: 1, cause: &llm.UpstreamError{StatusCode: 400}, want: false},
		{name: "request canceled", attemptCount: 1, cause: context.Canceled, want: false},
		{name: "no failure", attemptCount: 1, cause: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := canFailoverMessageRoute(
				test.attemptCount,
				test.visibleDeltaCount,
				test.attemptHadSideEffect,
				test.cause,
			)
			if got != test.want {
				t.Fatalf("canFailoverMessageRoute() = %v, want %v", got, test.want)
			}
		})
	}
}
