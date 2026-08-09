package contentmoderation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://api.openai.com", "https://api.openai.com/v1/moderations"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/moderations"},
		{"https://api.openai.com/v1/moderations", "https://api.openai.com/v1/moderations"},
		{"http://localhost:8080/proxy", "http://localhost:8080/proxy/v1/moderations"},
	}
	for _, tc := range cases {
		got, err := NormalizeBaseURL(tc.in)
		if err != nil {
			t.Fatalf("NormalizeBaseURL(%q) err=%v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeBaseURL(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitTextChunksUTF8(t *testing.T) {
	// Build a string larger than 12KiB with multi-byte runes.
	var b strings.Builder
	for b.Len() < maxTextChunkBytes+100 {
		b.WriteString("你好世界")
	}
	chunks := SplitTextChunks(b.String())
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if len([]byte(chunk)) > maxTextChunkBytes {
			t.Fatalf("chunk exceeds limit: %d", len([]byte(chunk)))
		}
	}
}

func TestEvaluateHitIgnoresTopLevelFlagged(t *testing.T) {
	resp := &Response{Results: []CategoryResult{{
		Flagged: true,
		Categories: map[string]bool{
			"hate":     false,
			"violence": false,
		},
		CategoryScores: map[string]float64{"hate": 0.9},
	}}}
	eval := EvaluateHit(resp, []string{"hate", "violence"}, ModalityText)
	if eval.Hit {
		t.Fatal("expected no hit when selected categories are false")
	}
}

func TestEvaluateHitSelectedCategory(t *testing.T) {
	resp := &Response{Results: []CategoryResult{{
		Flagged: false,
		Categories: map[string]bool{
			"violence": true,
			"hate":     true,
		},
		CategoryScores: map[string]float64{"violence": 0.8, "hate": 0.7},
		CategoryAppliedInputTypes: map[string][]string{
			"violence": {"text"},
			"hate":     {"text"},
		},
	}}}
	eval := EvaluateHit(resp, []string{"violence"}, ModalityText)
	if !eval.Hit || len(eval.Categories) != 1 || eval.Categories[0] != "violence" {
		t.Fatalf("unexpected eval: %+v", eval)
	}
}

func TestClientModerateText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/moderations" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Response{
			Model: "omni-moderation-latest",
			Results: []CategoryResult{{
				Categories:                map[string]bool{"hate": false},
				CategoryScores:            map[string]float64{"hate": 0.01},
				CategoryAppliedInputTypes: map[string][]string{"hate": {"text"}},
			}},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		Model:        "custom-moderation",
		TotalTimeout: 5 * time.Second,
		HTTPClient:   server.Client(),
	})
	resp, err := client.ModerateText(context.Background(), "hello", []string{"hate"}, ModalityText)
	if err != nil {
		t.Fatalf("ModerateText err=%v", err)
	}
	if resp.Model != "omni-moderation-latest" {
		t.Fatalf("model=%s", resp.Model)
	}
}

func TestMaskAPIKey(t *testing.T) {
	if MaskAPIKey("sk-abcdefghijklmnop") != "sk-a...mnop" {
		// prefix 4 + ... + suffix 4
		got := MaskAPIKey("sk-abcdefghijklmnop")
		if !strings.HasPrefix(got, "sk-a") || !strings.HasSuffix(got, "mnop") {
			t.Fatalf("mask=%s", got)
		}
	}
}

func TestNormalizeImageCategoriesRejectsTextOnly(t *testing.T) {
	_, err := NormalizePolicy(Policy{InputImageCategories: []string{"hate"}})
	if err == nil {
		t.Fatal("expected text-only category rejection")
	}
}

func TestModerateTextDoesNotSkipOnUnselectedHit(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// First chunk hits an unselected category; second hits selected violence.
		categories := map[string]bool{"hate": true, "violence": false}
		if calls >= 2 {
			categories = map[string]bool{"hate": false, "violence": true}
		}
		_ = json.NewEncoder(w).Encode(Response{
			Model: "omni-moderation-latest",
			Results: []CategoryResult{{
				Categories:                categories,
				CategoryScores:            map[string]float64{"hate": 0.9, "violence": 0.9},
				CategoryAppliedInputTypes: map[string][]string{"hate": {"text"}, "violence": {"text"}},
			}},
		})
	}))
	defer server.Close()

	// Force two chunks with a large payload.
	var b strings.Builder
	for b.Len() < maxTextChunkBytes+100 {
		b.WriteString("abcdefghij")
	}
	client := NewClient(ClientConfig{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		TotalTimeout: 5 * time.Second,
		HTTPClient:   server.Client(),
	})
	resp, err := client.ModerateText(context.Background(), b.String(), []string{"violence"}, ModalityText)
	if err != nil {
		t.Fatalf("ModerateText err=%v", err)
	}
	if calls < 2 {
		t.Fatalf("expected multi-chunk scan, got calls=%d", calls)
	}
	eval := EvaluateHit(resp, []string{"violence"}, ModalityText)
	if !eval.Hit {
		t.Fatalf("expected selected category hit after full scan, got %+v", eval)
	}
}
