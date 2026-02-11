package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// openImage opens an image using the system default viewer.
// For base64 data URLs, it creates a temp file first.
func openImage(img ImageRef) error {
	if img.IsBase64 {
		return openBase64Image(img.URL)
	}
	// Regular URL - open directly
	return exec.Command("open", img.URL).Start()
}

// openBase64Image decodes a base64 data URL and opens it.
func openBase64Image(dataURL string) error {
	// Parse data URL format: data:image/png;base64,<data>
	if !strings.HasPrefix(dataURL, "data:") {
		return fmt.Errorf("invalid data URL")
	}

	// Find the comma that separates metadata from data
	commaIdx := strings.Index(dataURL, ",")
	if commaIdx == -1 {
		return fmt.Errorf("invalid data URL format")
	}

	// Extract mime type and extension
	metadata := dataURL[5:commaIdx] // Skip "data:"
	mimeType := strings.Split(metadata, ";")[0]
	ext := getExtensionFromMimeType(mimeType)

	// Decode base64 data
	data, err := base64.StdEncoding.DecodeString(dataURL[commaIdx+1:])
	if err != nil {
		return fmt.Errorf("failed to decode base64: %w", err)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "llmproxy-image-*"+ext)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	// Write data
	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write image data: %w", err)
	}

	// Open with system viewer
	return exec.Command("open", tmpFile.Name()).Start()
}

// getExtensionFromMimeType returns a file extension for the given MIME type.
func getExtensionFromMimeType(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "image/bmp":
		return ".bmp"
	default:
		return ".png" // Default to png
	}
}

// extractAnthropicImageURL extracts image data from an Anthropic image content block.
// Anthropic format: {"type": "image", "source": {"type": "base64", "media_type": "...", "data": "..."}}
// or: {"type": "image", "source": {"type": "url", "url": "https://..."}}
func extractAnthropicImageURL(block map[string]interface{}) (string, bool) {
	source, ok := block["source"].(map[string]interface{})
	if !ok {
		return "", false
	}

	sourceType, _ := source["type"].(string)

	switch sourceType {
	case "base64":
		mediaType, _ := source["media_type"].(string)
		data, _ := source["data"].(string)
		if data != "" {
			if mediaType == "" {
				mediaType = "image/png"
			}
			url := fmt.Sprintf("data:%s;base64,%s", mediaType, data)
			return url, true
		}
	case "url":
		if url, ok := source["url"].(string); ok {
			return url, false
		}
	}

	return "", false
}

// extractImageURL extracts the URL from an image_url content block.
func extractImageURL(imageURLBlock map[string]interface{}) (string, bool) {
	if urlObj, ok := imageURLBlock["image_url"].(map[string]interface{}); ok {
		if url, ok := urlObj["url"].(string); ok {
			isBase64 := strings.HasPrefix(url, "data:")
			return url, isBase64
		}
	}
	return "", false
}

// truncateLongBase64Strings scans JSON bytes for long base64 string values
// and replaces them with truncated placeholders. This prevents TUI lag when
// displaying raw request/response JSON containing embedded images.
// Works with both data URLs (OpenAI format) and raw base64 strings (Anthropic format).
func truncateLongBase64Strings(jsonBytes []byte) []byte {
	const maxLen = 500

	var result []byte
	i := 0
	for i < len(jsonBytes) {
		if jsonBytes[i] != '"' {
			result = append(result, jsonBytes[i])
			i++
			continue
		}

		// Found opening quote
		result = append(result, '"')
		i++ // skip opening quote
		stringStart := i

		// Scan to end of string, handling escape sequences
		for i < len(jsonBytes) {
			if jsonBytes[i] == '\\' {
				i += 2 // skip escape sequence
			} else if jsonBytes[i] == '"' {
				break
			} else {
				i++
			}
		}

		stringContent := jsonBytes[stringStart:i]

		if isBase64String(stringContent, maxLen) {
			previewLen := min(80, len(stringContent))
			result = append(result, stringContent[:previewLen]...)
			msg := fmt.Sprintf("... [%d chars truncated]", len(stringContent)-previewLen)
			result = append(result, []byte(msg)...)
		} else {
			result = append(result, stringContent...)
		}

		// Add closing quote
		result = append(result, '"')
		if i < len(jsonBytes) {
			i++ // skip closing quote
		}
	}
	return result
}

// isBase64String checks if a JSON string value is likely base64 data that should be truncated.
func isBase64String(content []byte, maxLen int) bool {
	if len(content) <= maxLen {
		return false
	}
	// Data URLs (e.g., "data:image/png;base64,...") are always truncated when long
	if len(content) > 5 && string(content[:5]) == "data:" {
		return true
	}
	// Check if first 100 chars are all base64-safe characters (A-Za-z0-9+/=)
	sampleLen := min(100, len(content))
	for _, c := range content[:sampleLen] {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return true
}

// cleanupTempImages removes any temp image files we created.
// Called on program exit.
func cleanupTempImages() {
	pattern := filepath.Join(os.TempDir(), "llmproxy-image-*")
	files, _ := filepath.Glob(pattern)
	for _, f := range files {
		os.Remove(f)
	}
}
