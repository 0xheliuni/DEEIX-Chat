package identityprovider

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/security"
)

func TestClientSelectsTrustedClientOnlyForConfiguredOrigin(t *testing.T) {
	client := New(security.NewStrictOutboundPolicy(true))
	trustedClient, err := client.clientFor(
		"http://localhost:8080/token",
		[]string{"http://localhost:8080/issuer"},
	)
	if err != nil {
		t.Fatalf("select configured private origin: %v", err)
	}
	if trustedClient == client.strictClient.client {
		t.Fatal("configured origin should use its locally trusted client")
	}

	strictClient, err := client.clientFor(
		"http://localhost:8081/token",
		[]string{"http://localhost:8080/issuer"},
	)
	if err != nil {
		t.Fatalf("select unrelated origin: %v", err)
	}
	if strictClient != client.strictClient.client {
		t.Fatal("different port must not inherit the configured origin trust")
	}
}

func TestTrustedClientCacheUsesBoundedLRUAndClosesEvictedClient(t *testing.T) {
	client := New(security.NewStrictOutboundPolicy(true))
	origins := make([]string, 0, trustedClientCacheLimit)
	for index := 0; index < trustedClientCacheLimit; index++ {
		endpoint := fmt.Sprintf("http://localhost:%d/issuer", 10000+index)
		if _, err := client.clientFor(endpoint, []string{endpoint}); err != nil {
			t.Fatalf("cache trusted origin %d: %v", index, err)
		}
		origin, err := httpOrigin(endpoint)
		if err != nil {
			t.Fatalf("normalize trusted origin %d: %v", index, err)
		}
		origins = append(origins, origin)
	}

	if _, err := client.clientFor(origins[0]+"/token", []string{origins[0] + "/issuer"}); err != nil {
		t.Fatalf("refresh least-recently-used origin: %v", err)
	}
	evictedClosed := false
	evictedElement := client.trustedClients[origins[1]]
	if evictedElement == nil {
		t.Fatal("expected least-recently-used origin to be cached before eviction")
	}
	evictedEntry := evictedElement.Value.(*trustedClientCacheEntry)
	evictedEntry.client.closeIdleConnections = func() { evictedClosed = true }

	newEndpoint := "http://localhost:20000/issuer"
	if _, err := client.clientFor(newEndpoint, []string{newEndpoint}); err != nil {
		t.Fatalf("cache replacement trusted origin: %v", err)
	}
	if len(client.trustedClients) != trustedClientCacheLimit || client.trustedLRU.Len() != trustedClientCacheLimit {
		t.Fatalf("trusted client cache exceeded limit: map=%d lru=%d", len(client.trustedClients), client.trustedLRU.Len())
	}
	if client.trustedClients[origins[0]] == nil {
		t.Fatal("recently used origin should remain cached")
	}
	if client.trustedClients[origins[1]] != nil {
		t.Fatal("least-recently-used origin should be evicted")
	}
	if !evictedClosed {
		t.Fatal("evicted trusted client must close its idle connections")
	}
}

func TestTrustedClientCacheIsSafeUnderConcurrentAccess(t *testing.T) {
	client := New(security.NewStrictOutboundPolicy(true))
	const workers = 256
	errorsCh := make(chan error, workers)
	var waitGroup sync.WaitGroup
	for index := 0; index < workers; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			endpoint := fmt.Sprintf("http://localhost:%d/issuer", 10000+(index%80))
			_, err := client.clientFor(endpoint, []string{endpoint})
			errorsCh <- err
		}(index)
	}
	waitGroup.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent trusted origin lookup: %v", err)
		}
	}
	if len(client.trustedClients) > trustedClientCacheLimit || client.trustedLRU.Len() > trustedClientCacheLimit {
		t.Fatalf("concurrent access exceeded cache limit: map=%d lru=%d", len(client.trustedClients), client.trustedLRU.Len())
	}
}

func TestTrustedClientRejectsCrossOriginRedirect(t *testing.T) {
	client := New(security.NewStrictOutboundPolicy(true))
	trustedClient, err := client.clientFor(
		"http://localhost:8080/token",
		[]string{"http://localhost:8080/issuer"},
	)
	if err != nil {
		t.Fatalf("select configured private origin: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "http://localhost:8081/redirected", nil)
	if err != nil {
		t.Fatalf("build redirect request: %v", err)
	}
	if err = trustedClient.CheckRedirect(request, nil); err == nil {
		t.Fatal("expected cross-origin redirect to be rejected")
	}
}

func TestClientCannotTrustMetadataEndpoint(t *testing.T) {
	client := New(security.NewStrictOutboundPolicy(true))
	_, err := client.clientFor(
		"http://169.254.169.254/latest/meta-data",
		[]string{"http://169.254.169.254/latest/meta-data"},
	)
	if !errors.Is(err, security.ErrInvalidOutboundPolicy) {
		t.Fatalf("expected metadata endpoint to remain permanently blocked, got %v", err)
	}
}

func TestHTTPOriginNormalizesHostAndPreservesPort(t *testing.T) {
	origin, err := httpOrigin("HTTPS://Example.COM.:8443/path?query=1")
	if err != nil {
		t.Fatalf("normalize origin: %v", err)
	}
	if origin != "https://example.com:8443" {
		t.Fatalf("unexpected origin %q", origin)
	}
}

func TestHTTPOriginNormalizesDefaultPort(t *testing.T) {
	withPort, err := httpOrigin("https://example.com:443/issuer")
	if err != nil {
		t.Fatalf("normalize origin with default port: %v", err)
	}
	withoutPort, err := httpOrigin("https://example.com/token")
	if err != nil {
		t.Fatalf("normalize origin without port: %v", err)
	}
	if withPort != withoutPort {
		t.Fatalf("default port changed origin: with=%q without=%q", withPort, withoutPort)
	}
}
