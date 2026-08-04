package mediaartifact

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/security"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestNewClientDoesNotTraceSignedArtifactURLs(t *testing.T) {
	client := New(security.NewStrictOutboundPolicy(true))
	if _, ok := client.httpClient.Transport.(*http.Transport); !ok {
		t.Fatalf("media artifact transport must avoid generic URL tracing, got %T", client.httpClient.Transport)
	}
}

func TestDownloadImageReturnsBoundedArtifact(t *testing.T) {
	pngHeader := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	client := testClient(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://cdn.example.test/generated/image" {
			t.Fatalf("unexpected request URL: %s", request.URL)
		}
		return response(http.StatusOK, "image/png; charset=binary", pngHeader), nil
	}))

	data, mimeType, err := client.DownloadImage(t.Context(), "https://cdn.example.test/generated/image", int64(len(pngHeader)))
	if err != nil {
		t.Fatalf("download image: %v", err)
	}
	if string(data) != string(pngHeader) || mimeType != "image/png" {
		t.Fatalf("unexpected image result: data=%q MIME=%q", data, mimeType)
	}
}

func TestDownloadImageRejectsOversizedArtifact(t *testing.T) {
	client := testClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "image/png", []byte("1234")), nil
	}))

	_, _, err := client.DownloadImage(t.Context(), "https://cdn.example.test/generated/image", 3)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestDownloadImageRejectsGeminiFilesURI(t *testing.T) {
	client := testClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsupported Gemini image URI must not issue a request")
		return nil, nil
	}))

	_, _, err := client.DownloadImage(t.Context(), "https://generativelanguage.googleapis.com/v1beta/files/image_123", 1024)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported Gemini image error, got %v", err)
	}
}

func TestDownloadVideoPollsGeminiFileAndUsesResolvedMIME(t *testing.T) {
	requestURLs := make([]string, 0, 3)
	client := testClient(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestURLs = append(requestURLs, request.URL.String())
		if request.Header.Get(geminiAPIKeyHeader) != "secret" {
			t.Fatalf("missing Gemini API key for %s", request.URL)
		}
		switch len(requestURLs) {
		case 1:
			return response(http.StatusOK, "application/json", []byte(`{"state":"PROCESSING"}`)), nil
		case 2:
			return response(http.StatusOK, "application/json", []byte(`{"file":{"state":"ACTIVE","mimeType":"video/mp4"}}`)), nil
		case 3:
			return response(http.StatusOK, "application/octet-stream", []byte("video-bytes")), nil
		default:
			t.Fatalf("unexpected extra request: %s", request.URL)
			return nil, nil
		}
	}))
	client.pollAttempts = 3
	client.pollInterval = 0

	data, mimeType, err := client.DownloadVideo(
		t.Context(),
		"https://generativelanguage.googleapis.com/v1beta/files/video_123:download?alt=media&key=discarded",
		"secret",
		1024,
	)
	if err != nil {
		t.Fatalf("download Gemini video: %v", err)
	}
	if string(data) != "video-bytes" || mimeType != "video/mp4" {
		t.Fatalf("unexpected video result: data=%q MIME=%q", data, mimeType)
	}
	wantURLs := []string{
		"https://generativelanguage.googleapis.com/v1beta/files/video_123",
		"https://generativelanguage.googleapis.com/v1beta/files/video_123",
		"https://generativelanguage.googleapis.com/v1beta/files/video_123:download?alt=media",
	}
	if len(requestURLs) != len(wantURLs) {
		t.Fatalf("unexpected request count: %d", len(requestURLs))
	}
	for index := range wantURLs {
		if requestURLs[index] != wantURLs[index] {
			t.Fatalf("request %d: got %q, want %q", index, requestURLs[index], wantURLs[index])
		}
	}
}

func TestDownloadErrorDoesNotExposeSignedSourceURL(t *testing.T) {
	client := testClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	}))

	_, _, err := client.DownloadImage(t.Context(), "https://cdn.example.test/image?token=secret", 1024)
	if err == nil {
		t.Fatal("expected download failure")
	}
	if strings.Contains(err.Error(), "token=secret") {
		t.Fatalf("download error exposed signed URL: %v", err)
	}
	if !strings.Contains(err.Error(), "dial failed") {
		t.Fatalf("download error lost underlying cause: %v", err)
	}
}

func TestDownloadVideoRequiresGeminiAPIKeyBeforeRequest(t *testing.T) {
	client := testClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("missing API key must be rejected before issuing a request")
		return nil, nil
	}))

	_, _, err := client.DownloadVideo(
		t.Context(),
		"https://generativelanguage.googleapis.com/v1beta/files/video_123",
		"",
		1024,
	)
	if err == nil || !strings.Contains(err.Error(), "requires an API key") {
		t.Fatalf("expected missing API key error, got %v", err)
	}
}

func TestGeminiMetadataErrorDoesNotExposeResponseBody(t *testing.T) {
	client := testClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusBadGateway, "application/json", []byte(`{"error":"token=secret user-content"}`)), nil
	}))

	_, _, err := client.DownloadVideo(
		t.Context(),
		"https://generativelanguage.googleapis.com/v1beta/files/video_123",
		"api-key",
		1024,
	)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("expected status-only metadata error, got %v", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "user-content") {
		t.Fatalf("metadata error exposed response body: %v", err)
	}
}

func TestRedirectPolicyStripsGeminiKeyAcrossOrigins(t *testing.T) {
	originalURL, err := url.Parse("https://generativelanguage.googleapis.com/v1beta/files/video_123:download")
	if err != nil {
		t.Fatal(err)
	}
	redirectURL, err := url.Parse("https://storage.googleapis.com/generated/video_123")
	if err != nil {
		t.Fatal(err)
	}
	original := &http.Request{URL: originalURL, Header: make(http.Header)}
	original.Header.Set(geminiAPIKeyHeader, "secret")
	redirect := &http.Request{URL: redirectURL, Header: original.Header.Clone()}
	if err = stripCredentialOnCrossOriginRedirect(redirect, []*http.Request{original}); err != nil {
		t.Fatalf("check redirect: %v", err)
	}
	if redirect.Header.Get(geminiAPIKeyHeader) != "" {
		t.Fatal("Gemini API key leaked to a different origin")
	}

	sameOriginRedirect := &http.Request{URL: originalURL, Header: original.Header.Clone()}
	if err = stripCredentialOnCrossOriginRedirect(sameOriginRedirect, []*http.Request{original}); err != nil {
		t.Fatalf("check same-origin redirect: %v", err)
	}
	if sameOriginRedirect.Header.Get(geminiAPIKeyHeader) != "secret" {
		t.Fatal("same-origin redirect unexpectedly removed Gemini API key")
	}
}

func TestRedirectPolicyStopsAfterLimit(t *testing.T) {
	redirectURL, err := url.Parse("https://cdn.example.test/generated/video")
	if err != nil {
		t.Fatal(err)
	}
	request := &http.Request{URL: redirectURL, Header: make(http.Header)}
	via := make([]*http.Request, maxRedirects)
	for index := range via {
		via[index] = &http.Request{URL: redirectURL}
	}
	if err = stripCredentialOnCrossOriginRedirect(request, via); err == nil {
		t.Fatalf("expected redirect limit after %d redirects", maxRedirects)
	}
}

func TestStrictClientRejectsPrivateArtifactURL(t *testing.T) {
	client := New(security.NewStrictOutboundPolicy(true))
	_, _, err := client.DownloadImage(t.Context(), "http://127.0.0.1:8080/image.png", 1024)
	if !errors.Is(err, security.ErrUnsafeOutboundURL) {
		t.Fatalf("expected strict SSRF rejection, got %v", err)
	}
}

func TestDownloadRejectsURLUserInfoBeforeRequest(t *testing.T) {
	client := testClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("URL user info must be rejected before issuing a request")
		return nil, nil
	}))

	_, _, err := client.DownloadImage(t.Context(), "https://user:password@cdn.example.test/image", 1024)
	if !errors.Is(err, security.ErrUnsafeOutboundURL) {
		t.Fatalf("expected URL user info rejection, got %v", err)
	}
}

func TestCloseIdleConnectionsUsesOwnedTransport(t *testing.T) {
	closed := false
	client := &Client{closeIdleConnections: func() { closed = true }}
	client.CloseIdleConnections()
	if !closed {
		t.Fatal("expected idle connections to be closed")
	}
}

func testClient(transport http.RoundTripper) *Client {
	return &Client{
		httpClient: &http.Client{
			Transport:     transport,
			CheckRedirect: stripCredentialOnCrossOriginRedirect,
		},
		pollAttempts: 1,
		pollInterval: 0,
	}
}

func response(statusCode int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}
