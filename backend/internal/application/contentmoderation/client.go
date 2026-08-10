package contentmoderation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/security"
)

const (
	defaultModerationPath = "/moderations"
	maxTextChunkBytes     = 12 * 1024
	maxImageSourceBytes   = 20 * 1024 * 1024
	maxImageBatchBytes    = 20 * 1024 * 1024
	defaultHTTPTimeout    = 30 * time.Second
	maxRetryAfter         = 30 * time.Second
)

// ClientConfig configures the OpenAI-compatible moderations client.
type ClientConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	// TotalTimeout bounds queue wait + request + retry for a check surface.
	TotalTimeout   time.Duration
	HTTPClient     *http.Client
	OutboundPolicy security.OutboundPolicy
}

type moderationInput struct {
	Type     string              `json:"type"`
	Text     string              `json:"text,omitempty"`
	ImageURL *moderationImageURL `json:"image_url,omitempty"`
}

type moderationImageURL struct {
	URL string `json:"url"`
}

type moderationRequest struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"`
}

// CategoryResult is a single result item from the API.
type CategoryResult struct {
	Flagged                   bool
	Categories                map[string]bool
	CategoryScores            map[string]float64
	CategoryAppliedInputTypes map[string][]string
}

// Response is the OpenAI moderations response body.
type Response struct {
	ID      string
	Model   string
	Results []CategoryResult
}

type moderationCategoryResult struct {
	Flagged                   bool                `json:"flagged"`
	Categories                map[string]bool     `json:"categories"`
	CategoryScores            map[string]float64  `json:"category_scores"`
	CategoryAppliedInputTypes map[string][]string `json:"category_applied_input_types"`
}

type moderationResponse struct {
	ID      string                     `json:"id"`
	Model   string                     `json:"model"`
	Results []moderationCategoryResult `json:"results"`
}

func (document moderationResponse) toResponse() *Response {
	results := make([]CategoryResult, 0, len(document.Results))
	for _, result := range document.Results {
		results = append(results, CategoryResult{
			Flagged:                   result.Flagged,
			Categories:                result.Categories,
			CategoryScores:            result.CategoryScores,
			CategoryAppliedInputTypes: result.CategoryAppliedInputTypes,
		})
	}
	return &Response{ID: document.ID, Model: document.Model, Results: results}
}

// Client calls an OpenAI-compatible POST /v1/moderations endpoint.
type Client struct {
	cfg ClientConfig
}

// NewClient creates a moderation API client.
func NewClient(cfg ClientConfig) *Client {
	if cfg.HTTPClient == nil {
		policy := cfg.OutboundPolicy
		if endpoint, err := NormalizeBaseURL(cfg.BaseURL); err == nil {
			if trustedPolicy, trustErr := policy.WithTrustedHTTPURLs(endpoint); trustErr == nil {
				policy = trustedPolicy
			}
		}
		cfg.HTTPClient = security.NewOutboundHTTPClient(policy, defaultHTTPTimeout)
	}
	if cfg.TotalTimeout <= 0 {
		cfg.TotalTimeout = 10 * time.Second
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = "omni-moderation-latest"
	}
	return &Client{cfg: cfg}
}

// NormalizeBaseURL accepts API root, /v1, or full /moderations URL.
func NormalizeBaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ErrInvalidBaseURL
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", ErrInvalidBaseURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidBaseURL
	}
	path := strings.TrimRight(parsed.Path, "/")
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, "/moderations"):
	case strings.HasSuffix(lower, "/v1"):
		path = path + defaultModerationPath
	case path == "" || path == "/":
		path = "/v1" + defaultModerationPath
	default:
		path = path + "/v1" + defaultModerationPath
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

// SplitTextChunks splits text into UTF-8 safe chunks <= 12 KiB.
func SplitTextChunks(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	raw := []byte(text)
	if len(raw) <= maxTextChunkBytes {
		return []string{text}
	}
	chunks := make([]string, 0, (len(raw)/maxTextChunkBytes)+1)
	for len(raw) > 0 {
		limit := maxTextChunkBytes
		if limit > len(raw) {
			limit = len(raw)
		}
		for limit > 0 && !utf8.Valid(raw[:limit]) {
			limit--
		}
		if limit == 0 {
			_, size := utf8.DecodeRune(raw)
			if size <= 0 {
				size = 1
			}
			limit = size
		}
		chunks = append(chunks, string(raw[:limit]))
		raw = raw[limit:]
	}
	return chunks
}

// ModerateText checks one or more text chunks under the shared client timeout.
// selected limits early-exit: only a hit on an enabled category stops remaining chunks.
func (c *Client) ModerateText(ctx context.Context, text string, selected []string, modality string) (*Response, error) {
	chunks := SplitTextChunks(text)
	if len(chunks) == 0 {
		return &Response{Results: []CategoryResult{{
			Categories:                map[string]bool{},
			CategoryScores:            map[string]float64{},
			CategoryAppliedInputTypes: map[string][]string{},
		}}}, nil
	}
	if modality == "" {
		modality = ModalityText
	}
	deadline := time.Now().Add(c.cfg.TotalTimeout)
	var merged *Response
	for _, chunk := range chunks {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, ErrModerationTimeout
		}
		chunkCtx, cancel := context.WithTimeout(ctx, remaining)
		resp, err := c.moderate(chunkCtx, buildTextInput(chunk))
		cancel()
		if err != nil {
			return nil, err
		}
		merged = mergeModerationResponses(merged, resp)
		// Only stop early when an administrator-enabled category hit for this modality.
		if EvaluateHit(resp, selected, modality).Hit {
			return merged, nil
		}
	}
	return merged, nil
}

// ModerateImages checks image data URLs, batching by 20 MB total payload.
// selected limits early-exit across batches the same way as text chunks.
func (c *Client) ModerateImages(ctx context.Context, dataURLs []string, selected []string, modality string) (*Response, error) {
	if len(dataURLs) == 0 {
		return &Response{Results: []CategoryResult{{
			Categories:                map[string]bool{},
			CategoryScores:            map[string]float64{},
			CategoryAppliedInputTypes: map[string][]string{},
		}}}, nil
	}
	if modality == "" {
		modality = ModalityImage
	}
	batches := batchImageDataURLs(dataURLs)
	deadline := time.Now().Add(c.cfg.TotalTimeout)
	var merged *Response
	for _, batch := range batches {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, ErrModerationTimeout
		}
		batchCtx, cancel := context.WithTimeout(ctx, remaining)
		resp, err := c.moderate(batchCtx, buildImageInputs(batch))
		cancel()
		if err != nil {
			return nil, err
		}
		merged = mergeModerationResponses(merged, resp)
		if EvaluateHit(resp, selected, modality).Hit {
			return merged, nil
		}
	}
	return merged, nil
}

func mergeModerationResponses(base, next *Response) *Response {
	if base == nil {
		return next
	}
	if next == nil {
		return base
	}
	if len(base.Results) == 0 {
		return next
	}
	if len(next.Results) == 0 {
		return base
	}
	// Merge first result maps (Omni returns one result per request).
	left := &base.Results[0]
	right := next.Results[0]
	if left.Categories == nil {
		left.Categories = map[string]bool{}
	}
	if left.CategoryScores == nil {
		left.CategoryScores = map[string]float64{}
	}
	if left.CategoryAppliedInputTypes == nil {
		left.CategoryAppliedInputTypes = map[string][]string{}
	}
	for cat, flagged := range right.Categories {
		if flagged {
			left.Categories[cat] = true
		} else if _, exists := left.Categories[cat]; !exists {
			left.Categories[cat] = false
		}
	}
	for cat, score := range right.CategoryScores {
		if prev, ok := left.CategoryScores[cat]; !ok || score > prev {
			left.CategoryScores[cat] = score
		}
	}
	for cat, types := range right.CategoryAppliedInputTypes {
		left.CategoryAppliedInputTypes[cat] = mergeStringSets(left.CategoryAppliedInputTypes[cat], types)
	}
	if strings.TrimSpace(next.Model) != "" {
		base.Model = next.Model
	}
	return base
}

func mergeStringSets(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, item := range append(append([]string{}, a...), b...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (c *Client) moderate(ctx context.Context, input interface{}) (*Response, error) {
	endpoint, err := NormalizeBaseURL(c.cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(moderationRequest{
		Model: strings.TrimSpace(c.cfg.Model),
		Input: input,
	})
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, mapContextError(err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if key := strings.TrimSpace(c.cfg.APIKey); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}

		resp, err := c.cfg.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%w: %v", ErrModerationNetwork, err)
			if attempt == 0 && shouldRetryNetwork(err) {
				continue
			}
			return nil, lastErr
		}
		payload, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("%w: read body: %v", ErrModerationNetwork, readErr)
			if attempt == 0 {
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = mapHTTPStatus(resp.StatusCode, payload)
			if attempt == 0 {
				wait := parseRetryAfter(resp.Header.Get("Retry-After"))
				if wait > 0 {
					timer := time.NewTimer(wait)
					select {
					case <-ctx.Done():
						timer.Stop()
						return nil, mapContextError(ctx.Err())
					case <-timer.C:
					}
				}
				continue
			}
			return nil, lastErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, mapHTTPStatus(resp.StatusCode, payload)
		}

		var parsed moderationResponse
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrModerationInvalidResp, err)
		}
		if len(parsed.Results) == 0 {
			return nil, fmt.Errorf("%w: empty results", ErrModerationInvalidResp)
		}
		for i := range parsed.Results {
			if parsed.Results[i].Categories == nil {
				return nil, fmt.Errorf("%w: missing categories", ErrModerationInvalidResp)
			}
		}
		return parsed.toResponse(), nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrModerationService
}

func buildTextInput(text string) []moderationInput {
	return []moderationInput{{Type: "text", Text: text}}
}

func buildImageInputs(dataURLs []string) []moderationInput {
	items := make([]moderationInput, 0, len(dataURLs))
	for _, raw := range dataURLs {
		urlValue := strings.TrimSpace(raw)
		if urlValue == "" {
			continue
		}
		items = append(items, moderationInput{
			Type:     "image_url",
			ImageURL: &moderationImageURL{URL: urlValue},
		})
	}
	return items
}

func batchImageDataURLs(dataURLs []string) [][]string {
	batches := make([][]string, 0)
	current := make([]string, 0)
	currentSize := 0
	for _, item := range dataURLs {
		size := len(item)
		if size > maxImageSourceBytes {
			if len(current) > 0 {
				batches = append(batches, current)
				current = nil
				currentSize = 0
			}
			batches = append(batches, []string{item})
			continue
		}
		if len(current) > 0 && currentSize+size > maxImageBatchBytes {
			batches = append(batches, current)
			current = nil
			currentSize = 0
		}
		current = append(current, item)
		currentSize += size
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

func anyCategoryTrue(resp *Response) bool {
	if resp == nil {
		return false
	}
	for _, result := range resp.Results {
		for _, flagged := range result.Categories {
			if flagged {
				return true
			}
		}
	}
	return false
}

func mapHTTPStatus(status int, body []byte) error {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 200 {
		snippet = snippet[:200]
	}
	switch {
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", ErrModerationRateLimited, snippet)
	case status >= 500:
		return fmt.Errorf("%w: status %d %s", ErrModerationService, status, snippet)
	default:
		return fmt.Errorf("%w: status %d %s", ErrModerationService, status, snippet)
	}
}

func mapContextError(err error) error {
	if err == nil {
		return nil
	}
	if errorsIsTimeout(err) {
		return ErrModerationTimeout
	}
	return fmt.Errorf("%w: %v", ErrModerationNetwork, err)
}

func errorsIsTimeout(err error) bool {
	if err == nil {
		return false
	}
	if err == context.DeadlineExceeded {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout")
}

func shouldRetryNetwork(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "eof")
}

func parseRetryAfter(raw string) time.Duration {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		d := time.Duration(seconds) * time.Second
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	if t, err := http.ParseTime(value); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return 0
		}
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	return 0
}

// MaskAPIKey returns a display-safe key mask.
func MaskAPIKey(key string) string {
	value := strings.TrimSpace(key)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

// BuildImageDataURL builds a data URL from mime and raw bytes.
func BuildImageDataURL(mimeType string, data []byte) string {
	mime := strings.TrimSpace(mimeType)
	if mime == "" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}
