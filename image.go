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

// truncateBase64URL returns a shortened version of a base64 URL for display.
func truncateBase64URL(url string) string {
	if !strings.HasPrefix(url, "data:") {
		return url
	}
	// Find the comma
	commaIdx := strings.Index(url, ",")
	if commaIdx == -1 {
		return url
	}
	metadata := url[:commaIdx]
	dataLen := len(url) - commaIdx - 1
	return fmt.Sprintf("%s,[%d bytes base64]", metadata, dataLen)
}

// replaceBase64InJSON replaces base64 image data in JSON with placeholders.
// This is used to prevent lag when displaying raw request/response.
func replaceBase64InJSON(jsonBytes []byte, imageRefs []ImageRef) []byte {
	result := string(jsonBytes)

	for _, img := range imageRefs {
		if img.IsBase64 && len(img.URL) > 100 {
			// Find and replace the base64 data
			placeholder := fmt.Sprintf("[Image %d: %s]", img.Index, truncateBase64URL(img.URL))
			result = strings.Replace(result, img.URL, placeholder, 1)
		}
	}

	return []byte(result)
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
