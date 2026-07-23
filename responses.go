package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// isResponsesEndpoint returns true if the path is an OpenAI Responses API endpoint.
// Covers OpenAI (/v1/responses), OpenRouter (/api/v1/responses), and Azure (/openai/responses).
func isResponsesEndpoint(path string) bool {
	return strings.HasSuffix(path, "/responses")
}

// isResponsesSSEData returns true if SSE data appears to be a Responses API event stream.
func isResponsesSSEData(data []byte) bool {
	return bytes.Contains(data, []byte(`"type":"response.`)) ||
		bytes.Contains(data, []byte(`"type": "response.`)) ||
		bytes.Contains(data, []byte("event: response."))
}

// convertResponsesRequest parses a Responses API request body and converts it into
// the equivalent OpenAIRequest so existing chat-completions rendering can be reused.
// Returns nil if the body cannot be parsed.
func convertResponsesRequest(body []byte) *OpenAIRequest {
	var req struct {
		Model           string          `json:"model"`
		Input           json.RawMessage `json:"input"`
		Instructions    string          `json:"instructions"`
		Stream          bool            `json:"stream"`
		Temperature     float64         `json:"temperature"`
		MaxOutputTokens int             `json:"max_output_tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil
	}

	out := &OpenAIRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxOutputTokens,
		Stream:      req.Stream,
	}

	if req.Instructions != "" {
		out.Messages = append(out.Messages, OpenAIMessage{Role: "system", Content: req.Instructions})
	}

	if s := strings.TrimSpace(string(req.Input)); s != "" && s != "null" {
		var text string
		if json.Unmarshal(req.Input, &text) == nil {
			out.Messages = append(out.Messages, OpenAIMessage{Role: "user", Content: text})
		} else {
			var items []map[string]interface{}
			if json.Unmarshal(req.Input, &items) == nil {
				for _, item := range items {
					if msg, ok := responsesItemToMessage(item); ok {
						out.Messages = append(out.Messages, msg)
					}
				}
			}
		}
	}

	return out
}

// responsesItemToMessage converts a single Responses API input/output item into an OpenAI message.
func responsesItemToMessage(item map[string]interface{}) (OpenAIMessage, bool) {
	itemType, _ := item["type"].(string)
	role, _ := item["role"].(string)
	if itemType == "" && role != "" {
		itemType = "message"
	}

	switch itemType {
	case "message":
		if role == "" {
			role = "user"
		}
		return OpenAIMessage{Role: role, Content: convertResponsesContent(item["content"])}, true

	case "function_call":
		callID, _ := item["call_id"].(string)
		name, _ := item["name"].(string)
		args, _ := item["arguments"].(string)
		return OpenAIMessage{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:       callID,
				Type:     "function",
				Function: ToolCallFunction{Name: name, Arguments: args},
			}},
		}, true

	case "function_call_output":
		callID, _ := item["call_id"].(string)
		return OpenAIMessage{
			Role:       "tool",
			ToolCallID: callID,
			Content:    convertResponsesContent(item["output"]),
		}, true

	case "reasoning":
		if text := extractResponsesReasoningText(item); text != "" {
			return OpenAIMessage{Role: "assistant", ReasoningContent: text}, true
		}
		return OpenAIMessage{}, false

	case "item_reference":
		id, _ := item["id"].(string)
		return OpenAIMessage{Role: "user", Content: fmt.Sprintf("[item_reference: %s]", id)}, true

	default:
		// Built-in tool calls (web_search_call, file_search_call, computer_call, ...)
		// and unknown item types: show the raw item JSON.
		if role == "" {
			role = "assistant"
		}
		data, err := json.Marshal(item)
		if err != nil {
			return OpenAIMessage{}, false
		}
		return OpenAIMessage{Role: role, Content: string(data)}, true
	}
}

// convertResponsesContent converts Responses API content (string or part array) into
// chat-completions-shaped content so vision/audio rendering works unchanged.
func convertResponsesContent(content interface{}) interface{} {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var parts []interface{}
		for _, raw := range c {
			part, ok := raw.(map[string]interface{})
			if !ok {
				parts = append(parts, raw)
				continue
			}
			partType, _ := part["type"].(string)
			switch partType {
			case "input_text", "output_text", "text", "summary_text", "reasoning_text":
				text, _ := part["text"].(string)
				parts = append(parts, map[string]interface{}{"type": "text", "text": text})
			case "refusal":
				refusal, _ := part["refusal"].(string)
				parts = append(parts, map[string]interface{}{"type": "text", "text": "[refusal] " + refusal})
			case "input_image":
				if url, ok := part["image_url"].(string); ok && url != "" {
					parts = append(parts, map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": url},
					})
				} else if fileID, ok := part["file_id"].(string); ok && fileID != "" {
					parts = append(parts, map[string]interface{}{"type": "text", "text": fmt.Sprintf("[image: file %s]", fileID)})
				}
			case "input_file":
				label, _ := part["filename"].(string)
				if label == "" {
					label, _ = part["file_id"].(string)
				}
				parts = append(parts, map[string]interface{}{"type": "text", "text": fmt.Sprintf("[file: %s]", label)})
			default:
				parts = append(parts, raw)
			}
		}
		return parts
	default:
		return content
	}
}

// extractResponsesReasoningText pulls the summary/content text out of a reasoning item.
func extractResponsesReasoningText(item map[string]interface{}) string {
	var texts []string
	collect := func(key string) {
		blocks, ok := item[key].([]interface{})
		if !ok {
			return
		}
		for _, raw := range blocks {
			if block, ok := raw.(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok && text != "" {
					texts = append(texts, text)
				}
			}
		}
	}
	collect("summary")
	collect("content")
	return strings.Join(texts, "\n\n")
}

// responsesOutputToChoice converts a Responses API output item array into a single
// assistant choice, accumulating text, reasoning, and tool calls.
func responsesOutputToChoice(output []map[string]interface{}) OpenAIChoice {
	choice := OpenAIChoice{Index: 0}
	choice.Message.Role = "assistant"

	var textParts []string
	var reasoningParts []string

	for _, item := range output {
		itemType, _ := item["type"].(string)
		switch itemType {
		case "message":
			if text := extractOpenAITextContent(convertResponsesContent(item["content"])); text != "" {
				textParts = append(textParts, text)
			}
		case "function_call":
			callID, _ := item["call_id"].(string)
			name, _ := item["name"].(string)
			args, _ := item["arguments"].(string)
			choice.Message.ToolCalls = append(choice.Message.ToolCalls, ToolCall{
				ID:       callID,
				Type:     "function",
				Function: ToolCallFunction{Name: name, Arguments: args},
			})
		case "reasoning":
			if text := extractResponsesReasoningText(item); text != "" {
				reasoningParts = append(reasoningParts, text)
			}
		case "":
			// skip
		default:
			// Built-in tool invocations (web_search_call, file_search_call, ...)
			textParts = append(textParts, fmt.Sprintf("[%s]", itemType))
		}
	}

	choice.Message.Content = strings.Join(textParts, "\n\n")
	choice.Message.ReasoningContent = strings.Join(reasoningParts, "\n\n")
	return choice
}

// convertResponsesResponse parses a non-streaming Responses API response body and
// converts it into the equivalent OpenAIResponse. Returns nil if the body does not
// look like a Responses API response.
func convertResponsesResponse(body []byte) *OpenAIResponse {
	var resp struct {
		ID                string                   `json:"id"`
		Object            string                   `json:"object"`
		CreatedAt         float64                  `json:"created_at"`
		Model             string                   `json:"model"`
		Status            string                   `json:"status"`
		Output            []map[string]interface{} `json:"output"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	if resp.Object != "response" && len(resp.Output) == 0 {
		return nil
	}

	choice := responsesOutputToChoice(resp.Output)

	// Surface terminal errors (status "failed") in the choice text
	if resp.Error != nil && resp.Error.Message != "" {
		errText := "[error] " + resp.Error.Message
		if s, ok := choice.Message.Content.(string); ok && s != "" {
			choice.Message.Content = s + "\n\n" + errText
		} else {
			choice.Message.Content = errText
		}
	}

	switch {
	case len(choice.Message.ToolCalls) > 0:
		choice.FinishReason = "tool_calls"
	case resp.Status == "completed":
		choice.FinishReason = "stop"
	case resp.Status == "incomplete" && resp.IncompleteDetails != nil:
		choice.FinishReason = resp.IncompleteDetails.Reason
	default:
		choice.FinishReason = resp.Status
	}

	out := &OpenAIResponse{
		ID:      resp.ID,
		Object:  resp.Object,
		Created: int64(resp.CreatedAt),
		Model:   resp.Model,
		Choices: []OpenAIChoice{choice},
	}
	out.Usage.PromptTokens = resp.Usage.InputTokens
	out.Usage.CompletionTokens = resp.Usage.OutputTokens
	out.Usage.TotalTokens = resp.Usage.TotalTokens
	if out.Usage.TotalTokens == 0 {
		out.Usage.TotalTokens = out.Usage.PromptTokens + out.Usage.CompletionTokens
	}
	return out
}

// reassembleResponsesSSEResponse reconstructs an OpenAIResponse from a Responses API
// SSE event stream. It prefers the full response object carried by the terminal
// response.completed/failed/incomplete event; for partial (live) streams it falls back
// to accumulating output items and text/argument deltas.
func reassembleResponsesSSEResponse(data []byte) *OpenAIResponse {
	type itemState struct {
		itemType  string
		role      string
		callID    string
		name      string
		text      strings.Builder
		refusal   strings.Builder
		args      strings.Builder
		reasoning strings.Builder
		doneItem  map[string]interface{}
	}

	items := make(map[int]*itemState)
	getItem := func(idx int) *itemState {
		if st, ok := items[idx]; ok {
			return st
		}
		st := &itemState{}
		items[idx] = st
		return st
	}

	var finalResponse json.RawMessage
	var respID, respModel string

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		jsonData := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if jsonData == "" || jsonData == "[DONE]" {
			continue
		}

		var event struct {
			Type        string                 `json:"type"`
			OutputIndex int                    `json:"output_index"`
			Delta       json.RawMessage        `json:"delta"`
			Item        map[string]interface{} `json:"item"`
			Response    json.RawMessage        `json:"response"`
		}
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			continue
		}

		deltaText := func() string {
			var s string
			if json.Unmarshal(event.Delta, &s) == nil {
				return s
			}
			return ""
		}

		switch event.Type {
		case "response.completed", "response.failed", "response.incomplete":
			finalResponse = event.Response

		case "response.created", "response.in_progress":
			var meta struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			}
			if json.Unmarshal(event.Response, &meta) == nil {
				if meta.ID != "" {
					respID = meta.ID
				}
				if meta.Model != "" {
					respModel = meta.Model
				}
			}

		case "response.output_item.added":
			st := getItem(event.OutputIndex)
			st.itemType, _ = event.Item["type"].(string)
			st.role, _ = event.Item["role"].(string)
			st.callID, _ = event.Item["call_id"].(string)
			st.name, _ = event.Item["name"].(string)

		case "response.output_item.done":
			getItem(event.OutputIndex).doneItem = event.Item

		case "response.output_text.delta":
			getItem(event.OutputIndex).text.WriteString(deltaText())

		case "response.refusal.delta":
			getItem(event.OutputIndex).refusal.WriteString(deltaText())

		case "response.function_call_arguments.delta":
			getItem(event.OutputIndex).args.WriteString(deltaText())

		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			getItem(event.OutputIndex).reasoning.WriteString(deltaText())
		}
	}

	// Terminal event carries the complete response object. Only trust it wholesale
	// when it actually has output items — a failed/incomplete response can carry an
	// empty output array, and we'd otherwise discard the text accumulated from deltas.
	var final *OpenAIResponse
	if len(finalResponse) > 0 {
		final = convertResponsesResponse(finalResponse)
		var probe struct {
			Output []json.RawMessage `json:"output"`
		}
		if final != nil && json.Unmarshal(finalResponse, &probe) == nil && len(probe.Output) > 0 {
			return final
		}
	}

	if len(items) == 0 {
		return final
	}

	// Fallback: rebuild output items from accumulated deltas (live/partial streams).
	indices := make([]int, 0, len(items))
	for idx := range items {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	var output []map[string]interface{}
	for _, idx := range indices {
		st := items[idx]
		if st.doneItem != nil {
			output = append(output, st.doneItem)
			continue
		}
		switch st.itemType {
		case "function_call":
			output = append(output, map[string]interface{}{
				"type":      "function_call",
				"call_id":   st.callID,
				"name":      st.name,
				"arguments": st.args.String(),
			})
		case "reasoning":
			output = append(output, map[string]interface{}{
				"type":    "reasoning",
				"summary": []interface{}{map[string]interface{}{"type": "summary_text", "text": st.reasoning.String()}},
			})
		default:
			var content []interface{}
			if st.text.Len() > 0 {
				content = append(content, map[string]interface{}{"type": "output_text", "text": st.text.String()})
			}
			if st.refusal.Len() > 0 {
				content = append(content, map[string]interface{}{"type": "refusal", "refusal": st.refusal.String()})
			}
			if len(content) == 0 {
				continue
			}
			output = append(output, map[string]interface{}{
				"type":    "message",
				"role":    "assistant",
				"content": content,
			})
		}
	}

	if len(output) == 0 {
		return final
	}

	choice := responsesOutputToChoice(output)
	resp := &OpenAIResponse{
		ID:      respID,
		Object:  "response",
		Model:   respModel,
		Choices: []OpenAIChoice{choice},
	}

	// Merge authoritative metadata (usage, finish reason, surfaced error text) from
	// the terminal event when it carried no output items of its own.
	if final != nil {
		if final.ID != "" {
			resp.ID = final.ID
		}
		if final.Model != "" {
			resp.Model = final.Model
		}
		resp.Created = final.Created
		resp.Usage = final.Usage
		if len(final.Choices) > 0 {
			fc := final.Choices[0]
			if fc.FinishReason != "" {
				resp.Choices[0].FinishReason = fc.FinishReason
			}
			if s, ok := fc.Message.Content.(string); ok && s != "" {
				if cur, ok := resp.Choices[0].Message.Content.(string); ok && cur != "" {
					resp.Choices[0].Message.Content = cur + "\n\n" + s
				} else {
					resp.Choices[0].Message.Content = s
				}
			}
		}
	}
	return resp
}

// parseResponsesResponseBody converts a Responses API response body (JSON or SSE stream)
// into an OpenAIResponse. Returns nil if the body cannot be parsed.
func parseResponsesResponseBody(body []byte) *OpenAIResponse {
	if len(body) == 0 {
		return nil
	}
	if isSSEData(body) {
		return reassembleResponsesSSEResponse(body)
	}
	return convertResponsesResponse(body)
}
