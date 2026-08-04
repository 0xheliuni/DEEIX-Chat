package identityprovider

import (
	"container/list"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	platformtracing "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/observability/tracing"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/security"
)

const (
	requestTimeout          = 10 * time.Second
	maxResponseSize         = 1 << 20
	trustedClientCacheLimit = 64
)

// Response 是身份源 HTTP 调用经过边界化处理后的响应。
type Response struct {
	StatusCode int
	Status     string
	Body       []byte
}

// Successful 判断身份源是否返回 2xx 状态码。
func (r Response) Successful() bool {
	return r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices
}

// Client 统一承载身份源与人机验证服务的 HTTP、SSRF、重定向和响应大小边界。
// basePolicy 必须是不含全局私网白名单的严格策略；管理员配置的端点仅按精确 origin 局部授权。
type Client struct {
	basePolicy     security.OutboundPolicy
	strictClient   *managedHTTPClient
	trustedClients map[string]*list.Element
	trustedLRU     *list.List
	mu             sync.Mutex
}

type managedHTTPClient struct {
	client               *http.Client
	closeIdleConnections func()
}

type trustedClientCacheEntry struct {
	origin string
	client *managedHTTPClient
}

// New 创建身份源基础设施适配器。
func New(basePolicy security.OutboundPolicy) *Client {
	return &Client{
		basePolicy:     basePolicy,
		strictClient:   newHTTPClient(basePolicy, ""),
		trustedClients: make(map[string]*list.Element, trustedClientCacheLimit),
		trustedLRU:     list.New(),
	}
}

// Get 向身份源发送 GET 请求。
func (c *Client) Get(ctx context.Context, targetURL string, trustedEndpoints []string, headers map[string]string) (Response, error) {
	return c.do(ctx, http.MethodGet, targetURL, trustedEndpoints, headers, nil)
}

// PostForm 向身份源发送 application/x-www-form-urlencoded 请求。
func (c *Client) PostForm(ctx context.Context, targetURL string, trustedEndpoints []string, form url.Values, headers map[string]string) (Response, error) {
	requestHeaders := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		requestHeaders[key] = value
	}
	requestHeaders["Content-Type"] = "application/x-www-form-urlencoded"
	return c.do(ctx, http.MethodPost, targetURL, trustedEndpoints, requestHeaders, strings.NewReader(form.Encode()))
}

func (c *Client) do(
	ctx context.Context,
	method string,
	targetURL string,
	trustedEndpoints []string,
	headers map[string]string,
	body io.Reader,
) (Response, error) {
	if c == nil || c.strictClient == nil || c.strictClient.client == nil {
		return Response{}, fmt.Errorf("identity provider client is not configured")
	}
	client, err := c.clientFor(targetURL, trustedEndpoints)
	if err != nil {
		return Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimSpace(targetURL), body)
	if err != nil {
		return Response{}, fmt.Errorf("build identity provider request: %w", err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := client.Do(request)
	if err != nil {
		return Response{}, fmt.Errorf("request identity provider: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return Response{}, fmt.Errorf("read identity provider response: %w", err)
	}
	if len(responseBody) > maxResponseSize {
		return Response{}, fmt.Errorf("identity provider response exceeds %d bytes", maxResponseSize)
	}
	return Response{
		StatusCode: response.StatusCode,
		Status:     response.Status,
		Body:       responseBody,
	}, nil
}

func (c *Client) clientFor(targetURL string, trustedEndpoints []string) (*http.Client, error) {
	targetOrigin, err := httpOrigin(targetURL)
	if err != nil {
		return nil, err
	}
	trusted := false
	for _, endpoint := range trustedEndpoints {
		if strings.TrimSpace(endpoint) == "" {
			continue
		}
		endpointOrigin, originErr := httpOrigin(endpoint)
		if originErr != nil {
			return nil, fmt.Errorf("invalid configured identity provider endpoint: %w", originErr)
		}
		if endpointOrigin == targetOrigin {
			trusted = true
			break
		}
	}
	if !trusted {
		return c.strictClient.client, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if element := c.trustedClients[targetOrigin]; element != nil {
		c.trustedLRU.MoveToFront(element)
		return element.Value.(*trustedClientCacheEntry).client.client, nil
	}
	policy, err := c.basePolicy.WithTrustedHTTPURLs(targetURL)
	if err != nil {
		return nil, fmt.Errorf("trust configured identity provider endpoint: %w", err)
	}
	managedClient := newHTTPClient(policy, targetOrigin)
	element := c.trustedLRU.PushFront(&trustedClientCacheEntry{
		origin: targetOrigin,
		client: managedClient,
	})
	c.trustedClients[targetOrigin] = element
	c.evictTrustedClients()
	return managedClient.client, nil
}

func (c *Client) evictTrustedClients() {
	for c.trustedLRU.Len() > trustedClientCacheLimit {
		element := c.trustedLRU.Back()
		entry := element.Value.(*trustedClientCacheEntry)
		delete(c.trustedClients, entry.origin)
		c.trustedLRU.Remove(element)
		entry.client.closeIdle()
	}
}

// CloseIdleConnections 关闭严格客户端和所有可信 origin 客户端的空闲连接。
func (c *Client) CloseIdleConnections() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.strictClient != nil {
		c.strictClient.closeIdle()
	}
	if c.trustedLRU == nil {
		return
	}
	for element := c.trustedLRU.Front(); element != nil; element = element.Next() {
		element.Value.(*trustedClientCacheEntry).client.closeIdle()
	}
}

func (c *managedHTTPClient) closeIdle() {
	if c != nil && c.closeIdleConnections != nil {
		c.closeIdleConnections()
	}
}

func newHTTPClient(policy security.OutboundPolicy, trustedOrigin string) *managedHTTPClient {
	client := security.NewOutboundHTTPClient(policy, requestTimeout)
	baseTransport := client.Transport
	client.Transport = platformtracing.NewHTTPTransport(client.Transport)
	if trustedOrigin != "" {
		client.CheckRedirect = func(request *http.Request, _ []*http.Request) error {
			redirectOrigin, err := httpOrigin(request.URL.String())
			if err != nil || redirectOrigin != trustedOrigin {
				return fmt.Errorf("identity provider redirect changed trusted origin")
			}
			return nil
		}
	}
	managed := &managedHTTPClient{client: client}
	if transport, ok := baseTransport.(interface{ CloseIdleConnections() }); ok {
		managed.closeIdleConnections = transport.CloseIdleConnections
	}
	return managed
}

func httpOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("invalid HTTP endpoint")
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("endpoint must use HTTP or HTTPS")
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "" || strings.Contains(hostname, "%") {
		return "", fmt.Errorf("invalid HTTP endpoint host")
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port := parsed.Port(); port != "" && !isDefaultPort(scheme, port) {
		host += ":" + port
	}
	return scheme + "://" + host, nil
}

func isDefaultPort(scheme string, port string) bool {
	return (scheme == "http" && port == "80") || (scheme == "https" && port == "443")
}
