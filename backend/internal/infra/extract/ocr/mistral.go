package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
)

const defaultMistralOCRModel = "mistral-ocr-latest"

type mistralOCRRequest struct {
	Model              string             `json:"model"`
	Document           mistralOCRDocument `json:"document"`
	Pages              []int              `json:"pages,omitempty"`
	IncludeImageBase64 bool               `json:"include_image_base64"`
	IncludeBlocks      bool               `json:"include_blocks"`
}

type mistralOCRDocument struct {
	Type        string `json:"type"`
	DocumentURL string `json:"document_url,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
}

type mistralOCRResponse struct {
	Pages []mistralOCRPage `json:"pages"`
}

type mistralOCRPage struct {
	Index    int    `json:"index"`
	Markdown string `json:"markdown"`
}

func (c *Client) extractTextWithMistral(ctx context.Context, req Request) (Response, error) {
	if c == nil || !c.mistral || c.httpClient == nil || strings.TrimSpace(c.baseURL) == "" {
		return Response{}, fmt.Errorf("ocr_unavailable")
	}
	model := strings.TrimSpace(c.model)
	if model == "" {
		model = defaultMistralOCRModel
	}

	data, err := os.ReadFile(strings.TrimSpace(req.AbsolutePath))
	if err != nil {
		return Response{}, err
	}
	payload := mistralOCRRequest{
		Model:              model,
		IncludeImageBase64: false,
		IncludeBlocks:      false,
	}
	if isImageRequest(req) {
		mimeType := strings.TrimSpace(req.MimeType)
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		payload.Document = mistralOCRDocument{
			Type:     "image_url",
			ImageURL: buildMistralDataURI(mimeType, data),
		}
	} else {
		payload.Document = mistralOCRDocument{
			Type:        "document_url",
			DocumentURL: buildMistralDataURI("application/pdf", data),
		}
		pageNumbers := resolveOCRPageNumbers(req.PageRanges)
		if len(pageNumbers) > 0 {
			payload.Pages = make([]int, 0, len(pageNumbers))
			for _, pageNumber := range pageNumbers {
				payload.Pages = append(payload.Pages, pageNumber-1)
			}
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(c.authToken); token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Response{}, mistralHTTPError(resp)
	}
	return parseMistralOCRResponse(io.LimitReader(resp.Body, 50*1024*1024))
}

func buildMistralDataURI(mimeType string, data []byte) string {
	return "data:" + strings.TrimSpace(mimeType) + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func mistralHTTPError(resp *http.Response) error {
	detailBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	detail := strings.TrimSpace(string(detailBytes))
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("ocr_unauthorized")
	case http.StatusForbidden:
		return fmt.Errorf("ocr_forbidden")
	case http.StatusUnprocessableEntity:
		if detail == "" {
			return fmt.Errorf("ocr_unprocessable")
		}
		return fmt.Errorf("ocr_unprocessable: %s", detail)
	default:
		if detail == "" {
			return fmt.Errorf("ocr_http_%d", resp.StatusCode)
		}
		return fmt.Errorf("ocr_http_%d: %s", resp.StatusCode, detail)
	}
}

func parseMistralOCRResponse(body io.Reader) (Response, error) {
	var payload mistralOCRResponse
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return Response{}, err
	}
	pages := make([]PageText, 0, len(payload.Pages))
	for _, page := range payload.Pages {
		text := normalizeOCRText(page.Markdown)
		if text == "" {
			continue
		}
		pages = append(pages, PageText{
			PageNumber: page.Index + 1,
			Text:       text,
		})
	}
	sort.SliceStable(pages, func(i, j int) bool {
		return pages[i].PageNumber < pages[j].PageNumber
	})
	if len(pages) == 0 {
		return Response{}, fmt.Errorf(errOCREmptyContent)
	}
	parts := make([]string, 0, len(pages))
	for _, page := range pages {
		parts = append(parts, page.Text)
	}
	return Response{
		Text:          strings.Join(parts, "\n\n"),
		RenderedPages: len(pages),
		Pages:         pages,
	}, nil
}
