package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// geminiInteractionsAdapter 实现 Google Gemini Interactions API。
type geminiInteractionsAdapter struct {
	client *Client
}

func (a *geminiInteractionsAdapter) Name() string { return AdapterGeminiInteractions }

func (a *geminiInteractionsAdapter) Generate(ctx context.Context, route RouteConfig, input GenerateInput) (*GenerateOutput, error) {
	return a.client.generateGeminiInteraction(ctx, route, input)
}

func (a *geminiInteractionsAdapter) GenerateStream(
	ctx context.Context,
	route RouteConfig,
	input GenerateInput,
	onEvent func(GenerateStreamEvent) error,
) (*GenerateOutput, error) {
	return a.client.generateGeminiInteractionStream(ctx, route, input, onEvent)
}

func (a *geminiInteractionsAdapter) ListModels(ctx context.Context, route RouteConfig) ([]ModelItem, error) {
	return a.client.listModelsGemini(ctx, route)
}

func (c *Client) generateGeminiInteraction(ctx context.Context, route RouteConfig, input GenerateInput) (*GenerateOutput, error) {
	base := geminiBaseURL(route)
	requestURL := buildGeminiInteractionsURL(base)
	requestBody, err := buildGeminiInteractionRequestBody(route, input)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, resolveReadTimeout(route.ReadTimeoutMS))
	defer cancel()

	req, err := c.newGeminiRequest(requestCtx, http.MethodPost, requestURL, bytes.NewReader(payload), route, &input)
	if err != nil {
		return nil, err
	}
	resp, err := c.doRouteGenerationRequest(route, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := readUpstreamBody(resp.Body)
	if err != nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil, MarkRequestAccepted(err)
		}
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseGeminiError(resp.StatusCode, body, upstreamDebugSnapshot(req, payload, resp, body))
	}
	output, err := parseGeminiInteractionOutput(body)
	if err != nil {
		return nil, MarkRequestAccepted(err)
	}
	return output, nil
}

func (c *Client) generateGeminiInteractionStream(
	ctx context.Context,
	route RouteConfig,
	input GenerateInput,
	onEvent func(GenerateStreamEvent) error,
) (*GenerateOutput, error) {
	base := geminiBaseURL(route)
	requestURL := buildGeminiInteractionsURL(base)
	requestBody, err := buildGeminiInteractionRequestBody(route, input)
	if err != nil {
		return nil, err
	}
	requestBody["stream"] = true
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	firstByteCtx, firstByteCancel := context.WithCancel(ctx)
	defer firstByteCancel()

	firstByteTimer := time.AfterFunc(resolveReadTimeout(route.ReadTimeoutMS), firstByteCancel)
	defer firstByteTimer.Stop()

	req, err := c.newGeminiRequest(firstByteCtx, http.MethodPost, requestURL, bytes.NewReader(payload), route, &input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.doRouteGenerationRequest(route, req)
	firstByteTimer.Stop()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := readUpstreamBody(resp.Body)
		return nil, parseGeminiError(resp.StatusCode, body, upstreamDebugSnapshot(req, payload, resp, body))
	}

	result := &GenerateOutput{
		ToolCalls: make([]ToolCall, 0),
	}
	idleReader := newIdleTimeoutReader(resp.Body, resolveStreamIdleTimeout(route.StreamIdleTimeoutMS))
	streamBody := newUpstreamBodyRecorder(idleReader)
	if err = consumeGeminiInteractionStream(streamBody, result, onEvent); err != nil {
		return nil, MarkRequestAccepted(attachUpstreamDebug(err, upstreamDebugSnapshot(req, payload, resp, streamErrorBody(streamBody, err))))
	}
	return result, nil
}

func buildGeminiInteractionsURL(base string) string {
	return buildGeminiEndpointURL(base, "/interactions")
}

func buildGeminiInteractionRequestBody(route RouteConfig, input GenerateInput) (map[string]interface{}, error) {
	model := strings.TrimSpace(route.UpstreamModel)
	if model == "" {
		return nil, fmt.Errorf("interaction model required")
	}
	interactionInput := buildGeminiInteractionInput(input.Messages)
	if geminiInteractionInputEmpty(interactionInput) {
		return nil, fmt.Errorf("interaction input required")
	}
	payload := map[string]interface{}{
		"model": model,
		"input": interactionInput,
	}
	if format := buildGeminiInteractionResponseFormat(route, input.Options); !geminiInteractionInputEmpty(format) {
		payload["response_format"] = format
	}
	if config := buildGeminiInteractionGenerationConfig(input.Options); len(config) > 0 {
		payload["generation_config"] = config
	}
	if instructions := strings.TrimSpace(input.Instructions); instructions != "" {
		payload["system_instruction"] = instructions
	}
	if previousID := strings.TrimSpace(input.PreviousResponseID); previousID != "" {
		payload["previous_interaction_id"] = previousID
	}
	providerTools, toolDefinitions, toolsEnabled, err := toolDeclarationsForInput(input)
	if err != nil {
		return nil, err
	}
	if toolsEnabled {
		appendToolDeclarations(payload, providerTools, buildGeminiInteractionTools(toolDefinitions))
	}
	applyProviderOptions(payload, input.Options, geminiInteractionsProtectedProviderOptionKeys()...)
	return payload, nil
}

func buildGeminiInteractionInput(messages []Message) interface{} {
	steps := make([]map[string]interface{}, 0, len(messages))
	for _, message := range messages {
		stepType := geminiInteractionStepType(message.Role)
		contentMessage := message
		contentMessage.ToolCalls = nil
		contentMessage.ToolResults = nil
		if stepType != "" {
			content := buildGeminiInteractionContent(contentMessage)
			if !geminiInteractionInputEmpty(content) {
				steps = append(steps, map[string]interface{}{
					"type":    stepType,
					"content": content,
				})
			}
		}
		for _, call := range message.ToolCalls {
			if functionCall := buildGeminiInteractionFunctionCall(call); len(functionCall) > 0 {
				steps = append(steps, functionCall)
			}
		}
		for _, result := range message.ToolResults {
			if functionResult := buildGeminiInteractionFunctionResult(result); len(functionResult) > 0 {
				steps = append(steps, functionResult)
			}
		}
	}
	if len(steps) == 1 && steps[0]["type"] == "user_input" {
		return steps[0]["content"]
	}
	return steps
}

func geminiInteractionStepType(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "assistant", "model":
		return "model_output"
	case "user", "tool":
		return "user_input"
	default:
		return ""
	}
}

func buildGeminiInteractionContent(message Message) interface{} {
	if len(message.Parts) == 0 {
		return strings.TrimSpace(message.Content)
	}
	items := make([]map[string]interface{}, 0, len(message.Parts)+1)
	if text := strings.TrimSpace(message.Content); text != "" {
		items = append(items, map[string]interface{}{"type": "text", "text": text})
	}
	for _, part := range message.Parts {
		switch part.Kind {
		case ContentPartText, ContentPartFile:
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			items = append(items, map[string]interface{}{"type": "text", "text": text})
		case ContentPartImage:
			if len(part.Data) == 0 {
				continue
			}
			mimeType := strings.TrimSpace(part.MimeType)
			if mimeType == "" {
				mimeType = "image/png"
			}
			items = append(items, map[string]interface{}{
				"type":      "image",
				"mime_type": mimeType,
				"data":      base64.StdEncoding.EncodeToString(part.Data),
			})
		}
	}
	if len(items) == 1 && items[0]["type"] == "text" {
		return strings.TrimSpace(getString(items[0]["text"]))
	}
	return items
}

func buildGeminiInteractionFunctionCall(call ToolCall) map[string]interface{} {
	name := strings.TrimSpace(call.ToolName)
	if name == "" {
		return nil
	}
	args := strings.TrimSpace(call.ArgumentsJSON)
	if args == "" {
		args = "{}"
	}
	arguments := map[string]interface{}{}
	if err := json.Unmarshal([]byte(args), &arguments); err != nil {
		arguments = map[string]interface{}{"arguments": args}
	}
	item := map[string]interface{}{
		"type":      "function_call",
		"name":      name,
		"arguments": arguments,
	}
	if id := strings.TrimSpace(call.ToolCallID); id != "" {
		item["id"] = id
	}
	return item
}

func buildGeminiInteractionFunctionResult(result ToolResult) map[string]interface{} {
	name := strings.TrimSpace(result.ToolName)
	if name == "" {
		return nil
	}
	item := map[string]interface{}{
		"type":   "function_result",
		"name":   name,
		"result": geminiInteractionFunctionResultContent(result),
	}
	if id := strings.TrimSpace(result.ToolCallID); id != "" {
		item["call_id"] = id
	}
	return item
}

func geminiInteractionFunctionResultContent(result ToolResult) []map[string]interface{} {
	output := map[string]interface{}{}
	raw := strings.TrimSpace(result.OutputJSON)
	if raw != "" {
		var decoded interface{}
		if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
			if payload, ok := decoded.(map[string]interface{}); ok {
				output = payload
			} else {
				output["content"] = decoded
			}
		} else {
			output["content"] = raw
		}
	}
	if errText := strings.TrimSpace(result.Error); errText != "" {
		output["error"] = errText
	}
	if len(output) == 0 {
		output["content"] = ""
	}
	text, err := json.Marshal(output)
	if err != nil {
		text = []byte(`{"content":""}`)
	}
	return []map[string]interface{}{{
		"type": "text",
		"text": string(text),
	}}
}

func geminiInteractionInputEmpty(value interface{}) bool {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case map[string]interface{}:
		return len(typed) == 0
	case []map[string]interface{}:
		return len(typed) == 0
	case []interface{}:
		return len(typed) == 0
	default:
		return value == nil
	}
}

func buildGeminiInteractionResponseFormat(route RouteConfig, options map[string]interface{}) interface{} {
	if rawFormats, ok := firstGeminiInteractionResponseFormatList(options); ok {
		items := make([]interface{}, 0, len(rawFormats))
		for _, rawFormat := range rawFormats {
			if format := normalizeGeminiInteractionResponseFormat(route, rawFormat); len(format) > 0 {
				items = append(items, format)
			}
		}
		if len(items) > 0 {
			return items
		}
	}
	raw := modelParamMap(options, "response_format")
	if len(raw) == 0 {
		raw = modelParamMap(options, "responseFormat")
	}
	return normalizeGeminiInteractionResponseFormat(route, raw)
}

func normalizeGeminiInteractionResponseFormat(route RouteConfig, raw map[string]interface{}) map[string]interface{} {
	responseType := geminiInteractionResponseType(getString(raw["type"]))
	if responseType == "" {
		switch normalizeEndpoint(route.Endpoint) {
		case EndpointImageGenerations, EndpointImageEdits:
			responseType = "image"
		}
	}
	if responseType == "" {
		return nil
	}
	format := map[string]interface{}{
		"type": responseType,
	}
	if responseType == "video" {
		format["delivery"] = "uri"
	}
	if aspectRatio := geminiInteractionAspectRatio(getString(raw["aspect_ratio"]), responseType); aspectRatio != "" {
		format["aspect_ratio"] = aspectRatio
	}
	if aspectRatio := geminiInteractionAspectRatio(getString(raw["aspectRatio"]), responseType); aspectRatio != "" {
		format["aspect_ratio"] = aspectRatio
	}
	if imageSize := geminiInteractionImageSize(getString(raw["image_size"])); imageSize != "" {
		format["image_size"] = imageSize
	}
	if imageSize := geminiInteractionImageSize(getString(raw["imageSize"])); imageSize != "" {
		format["image_size"] = imageSize
	}
	if mimeType := geminiInteractionMIMEType(getString(raw["mime_type"]), responseType); mimeType != "" {
		format["mime_type"] = mimeType
	}
	if mimeType := geminiInteractionMIMEType(getString(raw["mimeType"]), responseType); mimeType != "" {
		format["mime_type"] = mimeType
	}
	return format
}

func firstGeminiInteractionResponseFormatList(options map[string]interface{}) ([]map[string]interface{}, bool) {
	for _, key := range []string{"response_format", "responseFormat"} {
		value, ok := options[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []map[string]interface{}:
			items := make([]map[string]interface{}, 0, len(typed))
			for _, item := range typed {
				if len(item) > 0 {
					items = append(items, item)
				}
			}
			return items, len(items) > 0
		case []interface{}:
			items := make([]map[string]interface{}, 0, len(typed))
			for _, raw := range typed {
				item := asMap(raw)
				if len(item) > 0 {
					items = append(items, item)
				}
			}
			return items, len(items) > 0
		default:
			return nil, false
		}
	}
	return nil, false
}

func geminiInteractionResponseType(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "image":
		return "image"
	case "video":
		return "video"
	case "text":
		return "text"
	default:
		return ""
	}
}

func buildGeminiInteractionGenerationConfig(options map[string]interface{}) map[string]interface{} {
	config := map[string]interface{}{}
	raw := modelParamMap(options, "generation_config")
	if len(raw) == 0 {
		raw = modelParamMap(options, "generationConfig")
	}
	for key, value := range raw {
		if strings.TrimSpace(key) != "" {
			config[camelToSnakeGeminiInteractionKey(key)] = value
		}
	}
	if maxTokens, ok := firstGeminiIntOption(options, "max_output_tokens", "max_completion_tokens", "maxOutputTokens"); ok && maxTokens > 0 {
		config["max_output_tokens"] = maxTokens
	}
	if value, ok := modelParamFloat(options, "temperature"); ok {
		config["temperature"] = value
	}
	if value, ok := firstGeminiFloatOption(options, "top_p", "topP"); ok {
		config["top_p"] = value
	}
	if topK, ok := firstGeminiIntOption(options, "top_k", "topK"); ok && topK > 0 {
		config["top_k"] = topK
	}
	if stops := firstGeminiStringListOption(options, "stop", "stop_sequences", "stopSequences"); len(stops) > 0 {
		config["stop_sequences"] = stops
	}
	if level := firstGeminiStringOption(options, "thinking_level", "thinkingLevel"); level != "" {
		config["thinking_level"] = level
	}
	if videoConfig := buildGeminiInteractionVideoConfig(modelParamMap(raw, "video_config")); len(videoConfig) > 0 {
		config["video_config"] = videoConfig
	} else if videoConfig := buildGeminiInteractionVideoConfig(modelParamMap(raw, "videoConfig")); len(videoConfig) > 0 {
		config["video_config"] = videoConfig
	}
	return config
}

func buildGeminiInteractionVideoConfig(raw map[string]interface{}) map[string]interface{} {
	config := map[string]interface{}{}
	if task := geminiInteractionVideoTask(getString(raw["task"])); task != "" {
		config["task"] = task
	}
	return config
}

func geminiInteractionAspectRatio(value string, responseType string) string {
	normalized := strings.TrimSpace(value)
	switch responseType {
	case "video":
		switch normalized {
		case "9:16", "16:9":
			return normalized
		default:
			return ""
		}
	default:
		switch normalized {
		case "1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9", "1:4", "4:1", "1:8", "8:1":
			return normalized
		default:
			return ""
		}
	}
}

func geminiInteractionVideoTask(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "text_to_video":
		return "text_to_video"
	case "image_to_video":
		return "image_to_video"
	case "reference_to_video":
		return "reference_to_video"
	case "edit":
		return "edit"
	default:
		return ""
	}
}

func geminiInteractionImageSize(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "512", "1K", "2K", "4K":
		return strings.ToUpper(strings.TrimSpace(value))
	default:
		return ""
	}
}

func geminiInteractionMIMEType(value string, responseType string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	switch responseType {
	case "image":
		switch normalized {
		case "image/png", "image/jpeg", "image/webp":
			return normalized
		}
	case "video":
		if strings.HasPrefix(normalized, "video/") {
			return normalized
		}
	}
	return ""
}

func camelToSnakeGeminiInteractionKey(value string) string {
	switch strings.TrimSpace(value) {
	case "maxOutputTokens":
		return "max_output_tokens"
	case "topP":
		return "top_p"
	case "topK":
		return "top_k"
	case "stopSequences":
		return "stop_sequences"
	case "videoConfig":
		return "video_config"
	case "thinkingLevel":
		return "thinking_level"
	default:
		return value
	}
}

func buildGeminiInteractionTools(tools []ToolDefinition) []map[string]interface{} {
	if len(tools) == 0 {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		items = append(items, map[string]interface{}{
			"type":        "function",
			"name":        name,
			"description": strings.TrimSpace(tool.Description),
			"parameters":  decodeToolSchema(tool.InputSchema),
		})
	}
	return items
}

func geminiInteractionsProtectedProviderOptionKeys() []string {
	return []string{
		"input",
		"model",
		"response_format",
		"responseFormat",
		"thinking_level",
		"thinkingLevel",
		"generation_config",
		"generationConfig",
		"max_completion_tokens",
		"max_output_tokens",
		"maxOutputTokens",
		"previous_interaction_id",
		"previousInteractionId",
		"stop",
		"stopSequences",
		"stream",
		"system_instruction",
		"systemInstruction",
		"temperature",
		"tools",
		"top_k",
		"top_p",
		"topK",
		"topP",
	}
}

func firstString(payload map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(getString(payload[key])); value != "" {
			return value
		}
	}
	return ""
}

// consumeGeminiInteractionStream 按 SSE 事件边界消费 Interactions 流，并保留跨事件的工具步骤关联状态。
func consumeGeminiInteractionStream(
	reader io.Reader,
	result *GenerateOutput,
	onEvent func(GenerateStreamEvent) error,
) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxUpstreamBodyBytes)
	streamState := newGeminiInteractionStreamState()

	var dataLines []string
	flush := func() error {
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		if data == "" || data == "[DONE]" {
			return nil
		}
		parsed := make(map[string]interface{})
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			return nil
		}
		if err := parseStreamUpstreamError(parsed, data); err != nil {
			return err
		}
		return applyGeminiInteractionStreamEvent(parsed, result, streamState, onEvent)
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(line[len("data:"):], " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

type geminiInteractionStreamToolStep struct {
	callID string
	name   string
}

type geminiInteractionStreamState struct {
	toolSteps map[int64]geminiInteractionStreamToolStep
}

func newGeminiInteractionStreamState() *geminiInteractionStreamState {
	return &geminiInteractionStreamState{toolSteps: make(map[int64]geminiInteractionStreamToolStep)}
}

// applyGeminiInteractionStreamEvent 将单个官方 event_type 事件归并到统一生成结果并向会话层发送增量。
func applyGeminiInteractionStreamEvent(
	parsed map[string]interface{},
	result *GenerateOutput,
	streamState *geminiInteractionStreamState,
	onEvent func(GenerateStreamEvent) error,
) error {
	if result == nil {
		return nil
	}
	eventType := strings.TrimSpace(getString(parsed["event_type"]))
	if responseID := geminiInteractionStreamResponseID(parsed, eventType); responseID != "" {
		result.ResponseID = responseID
	}
	if finalPayload := geminiInteractionStreamFinalPayload(parsed, eventType); len(finalPayload) > 0 {
		return mergeGeminiInteractionStreamFinal(result, finalPayload, onEvent)
	}
	if delta := geminiInteractionStreamTextDelta(parsed); delta != "" {
		result.Text += delta
		if onEvent != nil {
			if err := onEvent(GenerateStreamEvent{
				Delta:      delta,
				ResponseID: result.ResponseID,
			}); err != nil {
				return err
			}
		}
	}
	if reasoning := parseGeminiInteractionReasoningDelta(parsed); reasoning != nil {
		mergeReasoningDeltaOutput(&result.Reasoning, reasoning)
		if onEvent != nil {
			if err := onEvent(GenerateStreamEvent{
				Reasoning:  reasoning,
				ResponseID: result.ResponseID,
			}); err != nil {
				return err
			}
		}
	}
	for _, call := range parseGeminiInteractionFunctionCalls(parsed) {
		result.ToolCalls = append(result.ToolCalls, call)
	}
	result.ToolCalls = dedupeGeminiInteractionToolCalls(result.ToolCalls)
	for _, call := range parseGeminiInteractionStreamServerToolCalls(parsed, streamState) {
		merged := appendGeminiInteractionServerToolCall(result, call)
		if onEvent != nil {
			if err := onEvent(GenerateStreamEvent{
				ServerToolCall: &merged,
				ResponseID:     result.ResponseID,
			}); err != nil {
				return err
			}
		}
	}
	result.ServerSideToolUsage = geminiInteractionServerToolUsage(result.ServerToolCalls)
	result.GeneratedImages = dedupeGeminiInteractionImages(append(result.GeneratedImages, extractGeminiInteractionGeneratedImages(parsed)...))
	result.GeneratedVideos = dedupeGeminiInteractionVideos(append(result.GeneratedVideos, extractGeminiInteractionGeneratedVideos(parsed)...))
	if usage := parseGeminiInteractionUsage(parsed); usage != (Usage{}) {
		result.Usage = usage
		if onEvent != nil {
			return onEvent(GenerateStreamEvent{
				Usage:      usage,
				ResponseID: result.ResponseID,
			})
		}
	}
	return nil
}

func geminiInteractionStreamResponseID(parsed map[string]interface{}, eventType string) string {
	if interaction := asMap(parsed["interaction"]); len(interaction) > 0 {
		return firstString(interaction, "id", "name")
	}
	if strings.HasPrefix(strings.ToLower(eventType), "interaction.") {
		return firstString(parsed, "id", "name", "interaction_id", "interactionId")
	}
	return firstString(parsed, "interaction_id", "interactionId")
}

func geminiInteractionStreamFinalPayload(parsed map[string]interface{}, eventType string) map[string]interface{} {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	if eventType != "interaction.completed" && eventType != "completed" {
		return nil
	}
	return parsed
}

// mergeGeminiInteractionStreamFinal 使用完成事件补齐文本、用量与工具轨迹，不重复发送已累计的文本增量。
func mergeGeminiInteractionStreamFinal(
	result *GenerateOutput,
	payload map[string]interface{},
	onEvent func(GenerateStreamEvent) error,
) error {
	finalOutput := parseGeminiInteractionPayload(payload)
	if finalOutput.ResponseID != "" {
		result.ResponseID = finalOutput.ResponseID
	}
	if finalOutput.Text != "" {
		delta := ""
		if result.Text == "" {
			delta = finalOutput.Text
		} else if strings.HasPrefix(finalOutput.Text, result.Text) && len(finalOutput.Text) > len(result.Text) {
			delta = finalOutput.Text[len(result.Text):]
		}
		result.Text = finalOutput.Text
		if delta != "" && onEvent != nil {
			if err := onEvent(GenerateStreamEvent{
				Delta:      delta,
				ResponseID: result.ResponseID,
			}); err != nil {
				return err
			}
		}
	}
	if finalOutput.Usage != (Usage{}) {
		result.Usage = finalOutput.Usage
		if onEvent != nil {
			if err := onEvent(GenerateStreamEvent{
				Usage:      finalOutput.Usage,
				ResponseID: result.ResponseID,
			}); err != nil {
				return err
			}
		}
	}
	result.ToolCalls = dedupeGeminiInteractionToolCalls(append(result.ToolCalls, finalOutput.ToolCalls...))
	mergeReasoningOutput(&result.Reasoning, finalOutput.Reasoning)
	if len(finalOutput.ServerToolCalls) > 0 {
		result.ServerToolCalls = mergeGeminiInteractionFinalServerToolCalls(result.ServerToolCalls, finalOutput.ServerToolCalls)
		result.ServerSideToolUsage = geminiInteractionServerToolUsage(result.ServerToolCalls)
		if onEvent != nil {
			for index := range result.ServerToolCalls {
				if err := onEvent(GenerateStreamEvent{
					ServerToolCall: &result.ServerToolCalls[index],
					ResponseID:     result.ResponseID,
				}); err != nil {
					return err
				}
			}
		}
	}
	result.GeneratedImages = dedupeGeminiInteractionImages(append(result.GeneratedImages, finalOutput.GeneratedImages...))
	result.GeneratedVideos = dedupeGeminiInteractionVideos(append(result.GeneratedVideos, finalOutput.GeneratedVideos...))
	return nil
}

func geminiInteractionStreamTextDelta(parsed map[string]interface{}) string {
	eventType := strings.ToLower(strings.TrimSpace(getString(parsed["event_type"])))
	if strings.Contains(eventType, "text") || strings.Contains(eventType, "output") {
		for _, key := range []string{"delta", "text", "output_text"} {
			if text := geminiInteractionTextDeltaFromValue(parsed[key]); text != "" {
				return text
			}
		}
	}
	return geminiInteractionTextDeltaFromValue(parsed["delta"])
}

func geminiInteractionTextDeltaFromValue(raw interface{}) string {
	switch typed := raw.(type) {
	case string:
		return typed
	case []interface{}:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := geminiInteractionTextDeltaFromValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]interface{}:
		itemType := strings.ToLower(strings.TrimSpace(getString(typed["type"])))
		switch itemType {
		case "text", "output_text", "model_output":
			if text := getString(typed["text"]); text != "" {
				return text
			}
			return geminiInteractionTextDeltaFromValue(typed["content"])
		case "thinking", "thought", "thought_summary", "thought_signature", "reasoning", "function_call", "function_result", "image", "video":
			return ""
		}
		if text := getString(typed["text"]); text != "" {
			return text
		}
		return geminiInteractionTextDeltaFromValue(typed["content"])
	default:
		return ""
	}
}

func parseGeminiInteractionOutput(body []byte) (*GenerateOutput, error) {
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	output := parseGeminiInteractionPayload(parsed)
	output.RawJSON = string(body)
	return output, nil
}

// parseGeminiInteractionPayload 将 Interactions 完整响应映射为项目统一的生成结果。
func parseGeminiInteractionPayload(parsed map[string]interface{}) *GenerateOutput {
	payload := parsed
	if interaction := asMap(parsed["interaction"]); len(interaction) > 0 {
		payload = interaction
	}
	usage := parseGeminiInteractionUsage(parsed)
	output := &GenerateOutput{
		ResponseID:      firstString(payload, "id", "name"),
		Text:            strings.TrimSpace(firstString(payload, "text", "output_text")),
		Reasoning:       parseGeminiInteractionReasoning(payload),
		Usage:           usage,
		ToolCalls:       parseGeminiInteractionFunctionCalls(payload),
		ServerToolCalls: parseGeminiInteractionServerToolCalls(payload),
		GeneratedImages: extractGeminiInteractionGeneratedImages(payload),
		GeneratedVideos: extractGeminiInteractionGeneratedVideos(payload),
	}
	output.ServerSideToolUsage = geminiInteractionServerToolUsage(output.ServerToolCalls)
	if output.Text == "" {
		output.Text = geminiInteractionTextFromOutput(payload["output"])
	}
	if output.Text == "" {
		output.Text = geminiInteractionTextFromSteps(payload["steps"])
	}
	for i := range output.GeneratedImages {
		if output.GeneratedImages[i].RevisedPrompt == "" {
			output.GeneratedImages[i].RevisedPrompt = output.Text
		}
	}
	return output
}

// parseGeminiInteractionUsage 按 Interactions 官方 usage 字段拆分缓存输入、输出和思考 token。
func parseGeminiInteractionUsage(parsed map[string]interface{}) Usage {
	payload := parsed
	if interaction := asMap(parsed["interaction"]); len(interaction) > 0 {
		payload = interaction
	}
	usage := asMap(payload["usage"])
	if len(usage) == 0 {
		usage = asMap(asMap(parsed["metadata"])["total_usage"])
	}
	if len(usage) == 0 {
		return Usage{}
	}
	totalInputTokens := toInt64(usage["total_input_tokens"])
	cacheReadTokens := toInt64(usage["total_cached_tokens"])
	return Usage{
		InputTokens:     nonCachedInputTokens(totalInputTokens, cacheReadTokens),
		OutputTokens:    toInt64(usage["total_output_tokens"]),
		CacheReadTokens: cacheReadTokens,
		ReasoningTokens: toInt64(usage["total_thought_tokens"]),
		ServiceTier:     strings.TrimSpace(getString(payload["service_tier"])),
		RawUsageJSON:    rawJSONFromValue(usage),
	}
}

// parseGeminiInteractionReasoning 提取完整响应中的 thought 摘要和校验签名。
func parseGeminiInteractionReasoning(parsed map[string]interface{}) *ReasoningOutput {
	result := &ReasoningOutput{}
	for _, raw := range asSlice(parsed["steps"]) {
		step := asMap(raw)
		if strings.TrimSpace(strings.ToLower(getString(step["type"]))) != "thought" {
			continue
		}
		result.Summary += extractReasoningDeltaText(step["summary"])
		if signature := strings.TrimSpace(getString(step["signature"])); signature != "" {
			result.Signature = signature
		}
	}
	if strings.TrimSpace(result.Summary) == "" && strings.TrimSpace(result.Signature) == "" {
		return nil
	}
	return result
}

// parseGeminiInteractionReasoningDelta 映射官方 thought_summary 与 thought_signature 流式增量。
func parseGeminiInteractionReasoningDelta(parsed map[string]interface{}) *ReasoningDelta {
	eventType := strings.ToLower(strings.TrimSpace(getString(parsed["event_type"])))
	switch eventType {
	case "step.start":
		step := asMap(parsed["step"])
		if strings.TrimSpace(strings.ToLower(getString(step["type"]))) != "thought" {
			return nil
		}
		return geminiInteractionReasoningDelta(eventType, step)
	case "step.delta":
		delta := asMap(parsed["delta"])
		switch strings.TrimSpace(strings.ToLower(getString(delta["type"]))) {
		case "thought_summary", "thought_signature":
			return geminiInteractionReasoningDelta(eventType, delta)
		}
	}
	return nil
}

func geminiInteractionReasoningDelta(eventType string, payload map[string]interface{}) *ReasoningDelta {
	payloadType := strings.TrimSpace(strings.ToLower(getString(payload["type"])))
	text := ""
	if payloadType == "thought" {
		text = extractReasoningDeltaText(payload["summary"])
	} else if payloadType == "thought_summary" {
		text = extractReasoningDeltaText(payload["content"])
	}
	signature := strings.TrimSpace(getString(payload["signature"]))
	if text == "" && signature == "" {
		return nil
	}
	return &ReasoningDelta{
		EventType: eventType,
		Kind:      "summary_text",
		Text:      text,
		Signature: signature,
	}
}

func rawJSONFromValue(value interface{}) string {
	if value == nil {
		return ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func parseGeminiInteractionFunctionCalls(parsed map[string]interface{}) []ToolCall {
	calls := make([]ToolCall, 0)
	walkGeminiInteractionFunctionCalls(parsed["steps"], &calls)
	walkGeminiInteractionFunctionCalls(parsed["output"], &calls)
	return dedupeGeminiInteractionToolCalls(calls)
}

// parseGeminiInteractionServerToolCalls 将完整响应中的调用步骤与结果步骤合并为稳定的服务端工具轨迹。
func parseGeminiInteractionServerToolCalls(parsed map[string]interface{}) []ToolCall {
	calls := make([]ToolCall, 0)
	for index, raw := range asSlice(parsed["steps"]) {
		call, isResult, ok := geminiInteractionServerToolCallFromStep(asMap(raw))
		if ok {
			if !isResult && call.ToolCallID == "" {
				call.ToolCallID = geminiInteractionStreamToolCallID(call.ToolName, int64(index))
			}
			appendUniqueToolCall(&calls, call)
		}
	}
	return calls
}

// parseGeminiInteractionStreamServerToolCalls 按步骤索引关联不携带调用 ID 的增量事件，避免同一次调用被拆成多条轨迹。
func parseGeminiInteractionStreamServerToolCalls(
	parsed map[string]interface{},
	state *geminiInteractionStreamState,
) []ToolCall {
	if state == nil {
		state = newGeminiInteractionStreamState()
	}
	index := toInt64(parsed["index"])
	switch strings.ToLower(strings.TrimSpace(getString(parsed["event_type"]))) {
	case "step.start":
		step := asMap(parsed["step"])
		call, isResult, ok := geminiInteractionServerToolCallFromStep(step)
		if !ok {
			return nil
		}
		callID := call.ToolCallID
		if !isResult && callID == "" {
			callID = geminiInteractionStreamToolCallID(call.ToolName, index)
			call.ToolCallID = callID
		}
		state.toolSteps[index] = geminiInteractionStreamToolStep{
			callID: callID,
			name:   call.ToolName,
		}
		if isResult && call.OutputJSON == "" && call.ErrorJSON == "" {
			return nil
		}
		return []ToolCall{call}
	case "step.delta":
		delta := asMap(parsed["delta"])
		call, _, ok := geminiInteractionServerToolCallFromStep(delta)
		if !ok {
			return nil
		}
		if step, exists := state.toolSteps[index]; exists {
			if call.ToolCallID == "" {
				call.ToolCallID = step.callID
			}
			if call.ToolName == "" {
				call.ToolName = step.name
				call.ToolType = step.name
			}
		}
		return []ToolCall{call}
	default:
		return nil
	}
}

// geminiInteractionServerToolCallFromStep 只解析 Interactions 原生托管工具步骤，并标记当前步骤是否为结果。
func geminiInteractionServerToolCallFromStep(step map[string]interface{}) (ToolCall, bool, bool) {
	name, isResult, ok := geminiInteractionServerToolStepType(getString(step["type"]))
	if !ok {
		return ToolCall{}, false, false
	}
	if isResult {
		result, hasResult := step["result"]
		if !hasResult {
			return ToolCall{
				ToolCallID: strings.TrimSpace(getString(step["call_id"])),
				ToolType:   name,
				ToolName:   name,
			}, true, true
		}
		call := ToolCall{
			ToolCallID:       strings.TrimSpace(getString(step["call_id"])),
			ToolType:         name,
			ToolName:         name,
			ThoughtSignature: strings.TrimSpace(getString(step["signature"])),
			Status:           "completed",
			OutputJSON:       rawJSONFromValue(result),
		}
		if failed, _ := step["is_error"].(bool); failed {
			call.Status = "error"
			call.ErrorJSON = call.OutputJSON
		}
		return call, true, true
	}
	return ToolCall{
		ToolCallID:       strings.TrimSpace(getString(step["id"])),
		ToolType:         name,
		ToolName:         name,
		ArgumentsJSON:    rawJSONFromValue(step["arguments"]),
		ThoughtSignature: strings.TrimSpace(getString(step["signature"])),
		Status:           "in_progress",
	}, false, true
}

func geminiInteractionStreamToolCallID(name string, index int64) string {
	return fmt.Sprintf("gemini_interaction_%s_%d", name, index)
}

func geminiInteractionServerToolStepType(stepType string) (name string, isResult bool, ok bool) {
	switch strings.ToLower(strings.TrimSpace(stepType)) {
	case "google_search_call":
		return "google_search", false, true
	case "google_search_result":
		return "google_search", true, true
	case "code_execution_call":
		return "code_execution", false, true
	case "code_execution_result":
		return "code_execution", true, true
	case "url_context_call":
		return "url_context", false, true
	case "url_context_result":
		return "url_context", true, true
	default:
		return "", false, false
	}
}

// appendGeminiInteractionServerToolCall 将流式调用与结果合并，并优先复用上游提供的调用 ID。
func appendGeminiInteractionServerToolCall(result *GenerateOutput, call ToolCall) ToolCall {
	if result == nil {
		return call
	}
	if call.ToolCallID == "" && (call.OutputJSON != "" || call.ErrorJSON != "") {
		for index := len(result.ServerToolCalls) - 1; index >= 0; index-- {
			current := result.ServerToolCalls[index]
			if current.ToolName == call.ToolName && current.OutputJSON == "" && current.ErrorJSON == "" {
				call.ToolCallID = current.ToolCallID
				break
			}
		}
	}
	appendUniqueToolCall(&result.ServerToolCalls, call)
	for _, current := range result.ServerToolCalls {
		if shouldMergeToolCall(current, call) {
			return current
		}
	}
	return call
}

// mergeGeminiInteractionFinalServerToolCalls 用完成事件的完整轨迹补齐增量状态，同时保留已对外发送的稳定 ID。
func mergeGeminiInteractionFinalServerToolCalls(current []ToolCall, final []ToolCall) []ToolCall {
	if len(current) == 0 {
		return final
	}
	merged := append([]ToolCall(nil), current...)
	matched := make([]bool, len(merged))
	for _, incoming := range final {
		matchIndex := -1
		if incoming.ToolCallID != "" {
			for index, existing := range merged {
				if !matched[index] && existing.ToolCallID == incoming.ToolCallID {
					matchIndex = index
					break
				}
			}
		}
		if matchIndex < 0 {
			for index, existing := range merged {
				if !matched[index] && existing.ToolName == incoming.ToolName {
					matchIndex = index
					break
				}
			}
		}
		if matchIndex < 0 {
			merged = append(merged, incoming)
			matched = append(matched, true)
			continue
		}
		stableID := merged[matchIndex].ToolCallID
		merged[matchIndex] = mergeToolCall(merged[matchIndex], incoming)
		if stableID != "" {
			merged[matchIndex].ToolCallID = stableID
		}
		matched[matchIndex] = true
	}
	return merged
}

func geminiInteractionServerToolUsage(calls []ToolCall) map[string]int64 {
	if len(calls) == 0 {
		return nil
	}
	usage := make(map[string]int64)
	for _, call := range calls {
		if name := strings.TrimSpace(call.ToolName); name != "" {
			usage[name]++
		}
	}
	return usage
}

func walkGeminiInteractionFunctionCalls(value interface{}, calls *[]ToolCall) {
	switch typed := value.(type) {
	case map[string]interface{}:
		if call, ok := geminiInteractionToolCallFromMap(typed); ok {
			*calls = append(*calls, call)
		}
		for _, child := range typed {
			walkGeminiInteractionFunctionCalls(child, calls)
		}
	case []interface{}:
		for _, child := range typed {
			walkGeminiInteractionFunctionCalls(child, calls)
		}
	}
}

func geminiInteractionToolCallFromMap(item map[string]interface{}) (ToolCall, bool) {
	if strings.TrimSpace(strings.ToLower(getString(item["type"]))) != "function_call" {
		return ToolCall{}, false
	}
	name := strings.TrimSpace(getString(item["name"]))
	if name == "" {
		return ToolCall{}, false
	}
	arguments := normalizeJSONString(item["arguments"])
	if arguments == "" {
		arguments = normalizeJSONString(item["args"])
	}
	if arguments == "" {
		arguments = "{}"
	}
	return ToolCall{
		ToolCallID:    firstString(item, "id", "call_id", "tool_call_id"),
		ToolType:      "function",
		ToolName:      name,
		ArgumentsJSON: arguments,
		Status:        "requested",
	}, true
}

func dedupeGeminiInteractionToolCalls(calls []ToolCall) []ToolCall {
	if len(calls) <= 1 {
		return calls
	}
	result := make([]ToolCall, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		key := strings.TrimSpace(call.ToolCallID)
		if key == "" {
			key = call.ToolName + "\x00" + call.ArgumentsJSON
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, call)
	}
	return result
}

func extractGeminiInteractionGeneratedImages(parsed map[string]interface{}) []GeneratedImage {
	images := make([]GeneratedImage, 0)
	walkGeminiInteractionImages(parsed["output"], &images)
	walkGeminiInteractionModelOutputContent(parsed["steps"], func(content interface{}) {
		walkGeminiInteractionImages(content, &images)
	})
	return dedupeGeminiInteractionImages(images)
}

func dedupeGeminiInteractionImages(images []GeneratedImage) []GeneratedImage {
	if len(images) <= 1 {
		return images
	}
	deduped := make([]GeneratedImage, 0, len(images))
	seen := make(map[string]struct{}, len(images))
	for _, image := range images {
		key := strings.TrimSpace(image.URL)
		if key == "" {
			key = strings.TrimSpace(image.B64JSON)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, image)
	}
	return deduped
}

func walkGeminiInteractionModelOutputContent(raw interface{}, walk func(interface{})) {
	if walk == nil {
		return
	}
	for _, rawStep := range asSlice(raw) {
		step := asMap(rawStep)
		if stepType := strings.TrimSpace(strings.ToLower(getString(step["type"]))); stepType != "" && stepType != "model_output" {
			continue
		}
		if content, ok := step["content"]; ok {
			walk(content)
			continue
		}
		walk(step)
	}
}

func walkGeminiInteractionImages(value interface{}, images *[]GeneratedImage) {
	switch typed := value.(type) {
	case map[string]interface{}:
		if image, ok := geminiImageFromInteractionMap(typed); ok {
			*images = append(*images, image)
		}
		for _, child := range typed {
			walkGeminiInteractionImages(child, images)
		}
	case []interface{}:
		for _, child := range typed {
			walkGeminiInteractionImages(child, images)
		}
	}
}

func geminiImageFromInteractionMap(item map[string]interface{}) (GeneratedImage, bool) {
	mimeType := strings.TrimSpace(firstString(item, "mime_type", "mimeType"))
	if mimeType == "" {
		if inlineData := asMap(item["inlineData"]); len(inlineData) > 0 {
			mimeType = strings.TrimSpace(firstString(inlineData, "mimeType", "mime_type"))
		}
	}
	if mimeType != "" && !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return GeneratedImage{}, false
	}

	url := strings.TrimSpace(firstString(item, "uri", "url", "file_uri", "fileUri"))
	b64 := strings.TrimSpace(firstString(item, "b64_json", "b64Json", "data"))
	if fileData := asMap(item["fileData"]); len(fileData) > 0 {
		if url == "" {
			url = strings.TrimSpace(firstString(fileData, "fileUri", "file_uri", "uri", "url"))
		}
		if mimeType == "" {
			mimeType = strings.TrimSpace(firstString(fileData, "mimeType", "mime_type"))
		}
	}
	if fileData := asMap(item["file_data"]); len(fileData) > 0 {
		if url == "" {
			url = strings.TrimSpace(firstString(fileData, "fileUri", "file_uri", "uri", "url"))
		}
		if mimeType == "" {
			mimeType = strings.TrimSpace(firstString(fileData, "mimeType", "mime_type"))
		}
	}
	if inlineData := asMap(item["inlineData"]); len(inlineData) > 0 {
		if b64 == "" {
			b64 = strings.TrimSpace(firstString(inlineData, "data", "b64_json", "b64Json"))
		}
		if mimeType == "" {
			mimeType = strings.TrimSpace(firstString(inlineData, "mimeType", "mime_type"))
		}
	}
	if inlineData := asMap(item["inline_data"]); len(inlineData) > 0 {
		if b64 == "" {
			b64 = strings.TrimSpace(firstString(inlineData, "data", "b64_json", "b64Json"))
		}
		if mimeType == "" {
			mimeType = strings.TrimSpace(firstString(inlineData, "mimeType", "mime_type"))
		}
	}
	if nested := asMap(item["image"]); len(nested) > 0 {
		image, ok := geminiImageFromInteractionMap(nested)
		if ok {
			return image, true
		}
	}
	itemType := strings.TrimSpace(strings.ToLower(firstString(item, "type")))
	if mimeType == "" && itemType == "image" {
		mimeType = "image/png"
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") || (url == "" && b64 == "") {
		return GeneratedImage{}, false
	}
	return GeneratedImage{
		URL:      url,
		B64JSON:  b64,
		MIMEType: mimeType,
	}, true
}

func extractGeminiInteractionGeneratedVideos(parsed map[string]interface{}) []GeneratedVideo {
	videos := make([]GeneratedVideo, 0)
	walkGeminiInteractionVideos(parsed["output"], &videos)
	walkGeminiInteractionModelOutputContent(parsed["steps"], func(content interface{}) {
		walkGeminiInteractionVideos(content, &videos)
	})
	return dedupeGeminiInteractionVideos(videos)
}

func dedupeGeminiInteractionVideos(videos []GeneratedVideo) []GeneratedVideo {
	if len(videos) <= 1 {
		return videos
	}
	deduped := make([]GeneratedVideo, 0, len(videos))
	seen := make(map[string]struct{}, len(videos))
	for _, video := range videos {
		key := strings.TrimSpace(video.URL)
		if key == "" {
			key = strings.TrimSpace(video.B64JSON)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, video)
	}
	return deduped
}

func walkGeminiInteractionVideos(value interface{}, videos *[]GeneratedVideo) {
	switch typed := value.(type) {
	case map[string]interface{}:
		if video, ok := geminiVideoFromMap(typed); ok {
			*videos = append(*videos, video)
		}
		for _, child := range typed {
			walkGeminiInteractionVideos(child, videos)
		}
	case []interface{}:
		for _, child := range typed {
			walkGeminiInteractionVideos(child, videos)
		}
	}
}

func geminiVideoFromMap(item map[string]interface{}) (GeneratedVideo, bool) {
	fileData := asMap(item["fileData"])
	if len(fileData) == 0 {
		fileData = asMap(item["file_data"])
	}
	inlineData := asMap(item["inlineData"])
	if len(inlineData) == 0 {
		inlineData = asMap(item["inline_data"])
	}
	mimeType := strings.TrimSpace(firstString(item, "mime_type", "mimeType"))
	if mimeType == "" {
		if len(inlineData) > 0 {
			mimeType = strings.TrimSpace(firstString(inlineData, "mimeType", "mime_type"))
		}
	}
	if mimeType != "" && !strings.HasPrefix(strings.ToLower(mimeType), "video/") {
		return GeneratedVideo{}, false
	}

	url := strings.TrimSpace(firstString(item, "uri", "url", "file_uri", "fileUri"))
	b64 := strings.TrimSpace(firstString(item, "b64_json", "b64Json", "data"))
	if len(fileData) > 0 {
		if url == "" {
			url = strings.TrimSpace(firstString(fileData, "fileUri", "file_uri", "uri", "url"))
		}
		if mimeType == "" {
			mimeType = strings.TrimSpace(firstString(fileData, "mimeType", "mime_type"))
		}
	}
	if len(inlineData) > 0 {
		if b64 == "" {
			b64 = strings.TrimSpace(firstString(inlineData, "data", "b64_json", "b64Json"))
		}
		if mimeType == "" {
			mimeType = strings.TrimSpace(firstString(inlineData, "mimeType", "mime_type"))
		}
	}
	if mimeType == "" {
		mimeType = "video/mp4"
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "video/") || (url == "" && b64 == "") {
		return GeneratedVideo{}, false
	}
	return GeneratedVideo{
		URL:      url,
		B64JSON:  b64,
		MIMEType: mimeType,
		FileName: strings.TrimSpace(firstString(item, "file_name", "fileName", "name")),
		DurationSeconds: generatedMediaDurationSeconds(
			item["duration_seconds"],
			item["durationSeconds"],
			item["duration"],
			fileData["duration_seconds"],
			fileData["durationSeconds"],
		),
	}, true
}

func geminiInteractionTextFromOutput(raw interface{}) string {
	items, ok := raw.([]interface{})
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(getString(asMap(item)["text"])); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func geminiInteractionTextFromSteps(raw interface{}) string {
	steps, ok := raw.([]interface{})
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(steps))
	for _, rawStep := range steps {
		step := asMap(rawStep)
		if stepType := strings.TrimSpace(strings.ToLower(getString(step["type"]))); stepType != "" && stepType != "model_output" {
			continue
		}
		parts = appendGeminiInteractionTextParts(parts, step["content"])
	}
	return strings.Join(parts, "\n\n")
}

func appendGeminiInteractionTextParts(parts []string, raw interface{}) []string {
	switch typed := raw.(type) {
	case string:
		if text := strings.TrimSpace(typed); text != "" {
			return append(parts, text)
		}
	case map[string]interface{}:
		if itemType := strings.TrimSpace(strings.ToLower(getString(typed["type"]))); itemType != "" && itemType != "text" {
			return parts
		}
		if text := strings.TrimSpace(getString(typed["text"])); text != "" {
			return append(parts, text)
		}
		if content, ok := typed["content"]; ok {
			parts = appendGeminiInteractionTextParts(parts, content)
		}
	case []interface{}:
		for _, child := range typed {
			parts = appendGeminiInteractionTextParts(parts, child)
		}
	}
	return parts
}
