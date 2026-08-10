package contentmoderation

import (
	"encoding/json"
	"strings"
	"testing"

	appcm "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/contentmoderation"
	domaincm "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/contentmoderation"
)

func TestServiceConfigDTOJSONContract(t *testing.T) {
	dto := toServiceConfigDTO(&appcm.ServiceConfig{
		BaseURL:        "https://api.openai.com/v1",
		APIKeyMasked:   "sk-a...mnop",
		HasAPIKey:      true,
		Model:          "omni-moderation-latest",
		TimeoutSeconds: 10,
		MaxConcurrency: 4,
		QueueCapacity:  256,
		Policy: appcm.Policy{
			InputTextCategories: []string{"hate"},
			Version:             2,
		},
		PolicyVersion: 2,
	})
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, field := range []string{`"baseUrl"`, `"apiKeyMasked"`, `"inputTextCategories"`, `"policyVersion"`} {
		if !strings.Contains(text, field) {
			t.Fatalf("config JSON missing %s: %s", field, text)
		}
	}
	if strings.Contains(text, `"BaseURL"`) || strings.Contains(text, `"APIKey"`) {
		t.Fatalf("config JSON leaked application field names: %s", text)
	}
}

func TestUpdateConfigRequestMapsToApplicationInput(t *testing.T) {
	var request updateConfigRequest
	if err := json.Unmarshal([]byte(`{"baseUrl":"https://example.com/v1","clearAPIKey":true,"policy":{"inputTextCategories":["hate"],"version":3}}`), &request); err != nil {
		t.Fatal(err)
	}
	input := request.toApplication()
	if input.BaseURL == nil || *input.BaseURL != "https://example.com/v1" || !input.ClearAPIKey {
		t.Fatalf("unexpected config input: %#v", input)
	}
	if input.Policy == nil || input.Policy.Version != 3 || len(input.Policy.InputTextCategories) != 1 {
		t.Fatalf("unexpected policy input: %#v", input.Policy)
	}
}

func TestEventDetailDTOOmitsEncryptedText(t *testing.T) {
	dto := toEventDetailDTO(&appcm.EventDetail{
		Event: domaincm.Event{
			PublicID:      "evt-1",
			EncryptedText: "secret-ciphertext",
		},
		DecryptedText: "reviewable text",
	}, "User 1", "user1")
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "secret-ciphertext") || strings.Contains(text, "EncryptedText") {
		t.Fatalf("event detail leaked encrypted text: %s", text)
	}
	if !strings.Contains(text, `"decryptedText":"reviewable text"`) {
		t.Fatalf("event detail contract changed: %s", text)
	}
}
