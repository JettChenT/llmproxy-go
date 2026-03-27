package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

// ExportMessage represents a single message in the JSONL export
type ExportMessage struct {
	RequestID    int               `json:"request_id"`
	Index        int               `json:"index"`
	Role         string            `json:"role"`
	Content      string            `json:"content,omitempty"`
	ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID   string            `json:"tool_call_id,omitempty"`
	Images       []ExportImageRef  `json:"images,omitempty"`
	Audio        []ExportAudioRef  `json:"audio,omitempty"`
	IsResponse   bool              `json:"is_response"`
	Model        string            `json:"model,omitempty"`
	Endpoint     string            `json:"endpoint,omitempty"`
	Timestamp    time.Time         `json:"timestamp"`
	InputTokens  int               `json:"input_tokens,omitempty"`
	OutputTokens int               `json:"output_tokens,omitempty"`
}

// ExportImageRef references an exported image file
type ExportImageRef struct {
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
}

// ExportAudioRef references an exported audio file
type ExportAudioRef struct {
	Filename   string `json:"filename"`
	MimeType   string `json:"mime_type"`
	Format     string `json:"format"`
	Transcript string `json:"transcript,omitempty"`
}

// exportChat exports the selected request's chat transcript to a temporary folder
func (m *model) exportChat() {
	if m.selected == nil {
		m.copyMessage = "✗ No request selected"
		m.copyMessageTime = time.Now()
		return
	}

	// Create temp directory
	exportDir, err := os.MkdirTemp("", fmt.Sprintf("llmproxy-export-%d-", m.selected.ID))
	if err != nil {
		m.copyMessage = fmt.Sprintf("✗ Failed to create temp dir: %v", err)
		m.copyMessageTime = time.Now()
		return
	}

	var messages []ExportMessage
	imageCounter := 0

	isAnthropic := isAnthropicEndpoint(m.selected.Path)

	// Extract request messages
	if len(m.selected.RequestBody) > 0 {
		if isAnthropic {
			messages, imageCounter = extractAnthropicExportMessages(m.selected, exportDir, imageCounter)
		} else {
			messages, imageCounter = extractOpenAIExportMessages(m.selected, exportDir, imageCounter)
		}
	}

	// Extract response messages
	if len(m.selected.ResponseBody) > 0 {
		var respMessages []ExportMessage
		if isAnthropic {
			respMessages, imageCounter = extractAnthropicResponseExportMessages(m.selected, exportDir, imageCounter)
		} else {
			respMessages, imageCounter = extractOpenAIResponseExportMessages(m.selected, exportDir, imageCounter)
		}
		messages = append(messages, respMessages...)
	}

	// Write JSONL file
	jsonlPath := filepath.Join(exportDir, "transcript.jsonl")
	f, err := os.Create(jsonlPath)
	if err != nil {
		m.copyMessage = fmt.Sprintf("✗ Failed to create JSONL: %v", err)
		m.copyMessageTime = time.Now()
		return
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	for _, msg := range messages {
		if err := encoder.Encode(msg); err != nil {
			m.copyMessage = fmt.Sprintf("✗ Failed to write JSONL: %v", err)
			m.copyMessageTime = time.Now()
			return
		}
	}

	// Write README
	readmePath := filepath.Join(exportDir, "README.md")
	readme := generateExportReadme(exportDir, len(messages), imageCounter)
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		m.copyMessage = fmt.Sprintf("✗ Failed to write README: %v", err)
		m.copyMessageTime = time.Now()
		return
	}

	// Copy path to clipboard
	if err := clipboard.WriteAll(exportDir); err != nil {
		m.copyMessage = fmt.Sprintf("✗ Exported but clipboard failed: %v", err)
		m.copyMessageTime = time.Now()
		return
	}

	m.copyMessage = fmt.Sprintf("✓ Exported to clipboard (%d msgs, %d imgs)", len(messages), imageCounter)
	m.copyMessageTime = time.Now()
}

func extractOpenAIExportMessages(req *LLMRequest, exportDir string, imageCounter int) ([]ExportMessage, int) {
	var oaiReq OpenAIRequest
	if err := json.Unmarshal(req.RequestBody, &oaiReq); err != nil {
		return nil, imageCounter
	}

	var messages []ExportMessage
	for i, msg := range oaiReq.Messages {
		em := ExportMessage{
			RequestID:  req.ID,
			Index:      i,
			Role:       msg.Role,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
			IsResponse: false,
			Model:      oaiReq.Model,
			Endpoint:   req.Path,
			Timestamp:  req.StartTime,
		}

		// Extract text content
		em.Content = extractOpenAITextContent(msg.Content)

		// Extract images from vision content
		if contentArr, ok := msg.Content.([]interface{}); ok {
			for _, block := range contentArr {
				blockMap, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				blockType, _ := blockMap["type"].(string)
				if blockType == "image_url" {
					url, isBase64 := extractImageURL(blockMap)
					if url != "" {
						imgRef := exportImageToFile(url, isBase64, exportDir, &imageCounter)
						if imgRef != nil {
							em.Images = append(em.Images, *imgRef)
						}
					}
				} else if blockType == "input_audio" {
					if data, format, ok := extractInputAudioData(blockMap); ok {
						audioRef := exportAudioToFile(data, format, "", exportDir, &imageCounter)
						if audioRef != nil {
							em.Audio = append(em.Audio, *audioRef)
						}
					}
				}
			}
		}

		messages = append(messages, em)
	}

	return messages, imageCounter
}

func extractOpenAIResponseExportMessages(req *LLMRequest, exportDir string, imageCounter int) ([]ExportMessage, int) {
	responseBody := req.ResponseBody
	var resp OpenAIResponse
	if err := json.Unmarshal(responseBody, &resp); err != nil {
		if isSSEData(responseBody) {
			if assembled := reassembleSSEResponse(responseBody); assembled != nil {
				resp = *assembled
			} else {
				return nil, imageCounter
			}
		} else {
			return nil, imageCounter
		}
	}

	var messages []ExportMessage
	for _, choice := range resp.Choices {
		em := ExportMessage{
			RequestID:    req.ID,
			Index:        choice.Index,
			Role:         choice.Message.Role,
			Content:      extractOpenAITextContent(choice.Message.Content),
			ToolCalls:    choice.Message.ToolCalls,
			IsResponse:   true,
			Model:        resp.Model,
			Endpoint:     req.Path,
			Timestamp:    req.StartTime.Add(req.Duration),
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}

		// Include reasoning content if present
		if choice.Message.ReasoningContent != "" {
			em.Content = "<reasoning>\n" + choice.Message.ReasoningContent + "\n</reasoning>\n\n" + em.Content
		}

		// Include audio output if present
		if choice.Message.Audio != nil && choice.Message.Audio.Data != "" {
			format := choice.Message.Audio.Format
			if format == "" {
				format = "wav"
			}
			audioRef := exportAudioToFile(choice.Message.Audio.Data, format, choice.Message.Audio.Transcript, exportDir, &imageCounter)
			if audioRef != nil {
				em.Audio = append(em.Audio, *audioRef)
			}
			if choice.Message.Audio.Transcript != "" && em.Content == "" {
				em.Content = choice.Message.Audio.Transcript
			}
		}

		messages = append(messages, em)
	}

	return messages, imageCounter
}

func extractAnthropicExportMessages(req *LLMRequest, exportDir string, imageCounter int) ([]ExportMessage, int) {
	var antReq AnthropicRequest
	if err := json.Unmarshal(req.RequestBody, &antReq); err != nil {
		return nil, imageCounter
	}

	var messages []ExportMessage

	// Handle system message
	if antReq.System != nil {
		systemText := ""
		switch sys := antReq.System.(type) {
		case string:
			systemText = sys
		case []interface{}:
			for _, block := range sys {
				if blockMap, ok := block.(map[string]interface{}); ok {
					if text, ok := blockMap["text"].(string); ok {
						systemText += text + "\n"
					}
				}
			}
		}
		if systemText != "" {
			messages = append(messages, ExportMessage{
				RequestID: req.ID,
				Index:     0,
				Role:      "system",
				Content:   strings.TrimSpace(systemText),
				IsResponse: false,
				Model:     antReq.Model,
				Endpoint:  req.Path,
				Timestamp: req.StartTime,
			})
		}
	}

	for i, msg := range antReq.Messages {
		em := ExportMessage{
			RequestID:  req.ID,
			Index:      i + 1,
			Role:       msg.Role,
			IsResponse: false,
			Model:      antReq.Model,
			Endpoint:   req.Path,
			Timestamp:  req.StartTime,
		}

		switch content := msg.Content.(type) {
		case string:
			em.Content = content
		case []interface{}:
			var textParts []string
			for _, block := range content {
				blockMap, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				blockType, _ := blockMap["type"].(string)
				switch blockType {
				case "text":
					if text, ok := blockMap["text"].(string); ok {
						textParts = append(textParts, text)
					}
				case "image":
					url, isBase64 := extractAnthropicImageURL(blockMap)
					if url != "" {
						imgRef := exportImageToFile(url, isBase64, exportDir, &imageCounter)
						if imgRef != nil {
							em.Images = append(em.Images, *imgRef)
						}
					}
				case "input_audio":
					if data, format, ok := extractInputAudioData(blockMap); ok {
						audioRef := exportAudioToFile(data, format, "", exportDir, &imageCounter)
						if audioRef != nil {
							em.Audio = append(em.Audio, *audioRef)
						}
					}
				case "tool_use":
					name, _ := blockMap["name"].(string)
					id, _ := blockMap["id"].(string)
					inputJSON, _ := json.Marshal(blockMap["input"])
					em.ToolCalls = append(em.ToolCalls, ToolCall{
						ID:   id,
						Type: "function",
						Function: ToolCallFunction{
							Name:      name,
							Arguments: string(inputJSON),
						},
					})
				case "tool_result":
					toolCallID, _ := blockMap["tool_call_id"].(string)
					em.ToolCallID = toolCallID
					if resultContent, ok := blockMap["content"].(string); ok {
						textParts = append(textParts, resultContent)
					} else if resultArr, ok := blockMap["content"].([]interface{}); ok {
						for _, rb := range resultArr {
							if rbMap, ok := rb.(map[string]interface{}); ok {
								if text, ok := rbMap["text"].(string); ok {
									textParts = append(textParts, text)
								}
							}
						}
					}
				}
			}
			em.Content = strings.Join(textParts, "\n")
		}

		messages = append(messages, em)
	}

	return messages, imageCounter
}

func extractAnthropicResponseExportMessages(req *LLMRequest, _ string, imageCounter int) ([]ExportMessage, int) {
	responseBody := req.ResponseBody
	var resp AnthropicResponse
	if err := json.Unmarshal(responseBody, &resp); err != nil {
		if isSSEData(responseBody) {
			if assembled := reassembleAnthropicSSEResponse(responseBody); assembled != nil {
				resp = *assembled
			} else {
				return nil, imageCounter
			}
		} else {
			return nil, imageCounter
		}
	}

	em := ExportMessage{
		RequestID:    req.ID,
		Index:        0,
		Role:         resp.Role,
		IsResponse:   true,
		Model:        resp.Model,
		Endpoint:     req.Path,
		Timestamp:    req.StartTime.Add(req.Duration),
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
	}

	var textParts []string
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "thinking":
			textParts = append(textParts, "<thinking>\n"+block.Thinking+"\n</thinking>")
		case "tool_use":
			inputJSON, _ := json.Marshal(block.Input)
			em.ToolCalls = append(em.ToolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: ToolCallFunction{
					Name:      block.Name,
					Arguments: string(inputJSON),
				},
			})
		}
	}
	em.Content = strings.Join(textParts, "\n\n")

	return []ExportMessage{em}, imageCounter
}

// exportImageToFile saves an image to the export directory and returns a reference
func exportImageToFile(url string, isBase64 bool, exportDir string, counter *int) *ExportImageRef {
	*counter++
	idx := *counter

	if isBase64 || strings.HasPrefix(url, "data:") {
		// Parse data URL
		commaIdx := strings.Index(url, ",")
		if commaIdx == -1 {
			return nil
		}

		metadata := url[5:commaIdx] // Skip "data:"
		mimeType := strings.Split(metadata, ";")[0]
		ext := getExtensionFromMimeType(mimeType)

		data, err := base64.StdEncoding.DecodeString(url[commaIdx+1:])
		if err != nil {
			return nil
		}

		filename := fmt.Sprintf("image_%d%s", idx, ext)
		filePath := filepath.Join(exportDir, filename)
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return nil
		}

		return &ExportImageRef{
			Filename: filePath,
			MimeType: mimeType,
		}
	}

	// For URL images, just reference the URL (don't download)
	return &ExportImageRef{
		Filename: url,
		MimeType: "image/url",
	}
}

// exportAudioToFile saves audio data to the export directory and returns a reference
func exportAudioToFile(data string, format string, transcript string, exportDir string, counter *int) *ExportAudioRef {
	*counter++
	idx := *counter

	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil
	}

	ext := getAudioExtension(format)
	filename := fmt.Sprintf("audio_%d%s", idx, ext)
	filePath := filepath.Join(exportDir, filename)
	if err := os.WriteFile(filePath, decoded, 0644); err != nil {
		return nil
	}

	return &ExportAudioRef{
		Filename:   filePath,
		MimeType:   getAudioMimeType(format),
		Format:     format,
		Transcript: transcript,
	}
}

func generateExportReadme(exportDir string, messageCount, imageCount int) string {
	return fmt.Sprintf(`# LLM Chat Transcript Export

**Exported:** %s
**Location:** %s
**Messages:** %d
**Images:** %d

## Files

- **transcript.jsonl** — Chat messages in JSON Lines format (one JSON object per line)
- **image_N.{png,jpg,...}** — Extracted images referenced by messages

## JSONL Schema

Each line in transcript.jsonl is a JSON object with these fields:

| Field | Type | Description |
|-------|------|-------------|
| request_id | int | The proxy request ID this message belongs to |
| index | int | Message index within the request |
| role | string | Message role: "system", "user", "assistant", "tool" |
| content | string | Text content of the message |
| tool_calls | array | Tool/function calls made (if any) |
| tool_call_id | string | ID of the tool call this message responds to |
| images | array | Image references: [{filename, mime_type}] |
| is_response | bool | true if this is from the LLM response, false if from the request |
| model | string | Model name (e.g., "gpt-4", "claude-3-opus") |
| endpoint | string | API endpoint path |
| timestamp | string | ISO 8601 timestamp |
| input_tokens | int | Input token count (response messages only) |
| output_tokens | int | Output token count (response messages only) |

## How to Query

### Read all messages
` + "```bash" + `
cat transcript.jsonl | jq .
` + "```" + `

### Get just the conversation text
` + "```bash" + `
cat transcript.jsonl | jq -r '[.role, .content] | @tsv'
` + "```" + `

### Filter by role
` + "```bash" + `
cat transcript.jsonl | jq -r 'select(.role == "assistant") | .content'
` + "```" + `

### Get response messages only
` + "```bash" + `
cat transcript.jsonl | jq -r 'select(.is_response == true) | .content'
` + "```" + `

### Find messages with images
` + "```bash" + `
cat transcript.jsonl | jq 'select(.images | length > 0)'
` + "```" + `

### Find messages with tool calls
` + "```bash" + `
cat transcript.jsonl | jq 'select(.tool_calls | length > 0) | {role, tool_calls}'
` + "```" + `

### Python
` + "```python" + `
import json

with open("transcript.jsonl") as f:
    messages = [json.loads(line) for line in f]

for msg in messages:
    print(f"[{msg['role']}] {msg.get('content', '')[:100]}")
    for img in msg.get("images", []):
        print(f"  Image: {img['filename']}")
` + "```" + `

### Load into a Claude Code / agent context
Point the agent at this directory:
` + "```" + `
Read the transcript at %s/transcript.jsonl and the README at %s/README.md
` + "```" + `
`, time.Now().Format(time.RFC3339), exportDir, messageCount, imageCount, exportDir, exportDir)
}
