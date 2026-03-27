package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// openAudio decodes base64 audio data and opens it with the system player.
func openAudio(audio AudioRef) error {
	data, err := base64.StdEncoding.DecodeString(audio.Data)
	if err != nil {
		return fmt.Errorf("failed to decode base64 audio: %w", err)
	}

	ext := getAudioExtension(audio.Format)
	tmpFile, err := os.CreateTemp("", "llmproxy-audio-*"+ext)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write audio data: %w", err)
	}

	return exec.Command("open", tmpFile.Name()).Start()
}

// getAudioExtension returns a file extension for the given audio format.
func getAudioExtension(format string) string {
	switch format {
	case "wav":
		return ".wav"
	case "mp3":
		return ".mp3"
	case "aac":
		return ".aac"
	case "ogg":
		return ".ogg"
	case "flac":
		return ".flac"
	case "m4a":
		return ".m4a"
	case "aiff":
		return ".aiff"
	case "opus":
		return ".opus"
	case "pcm16", "pcm24":
		return ".pcm"
	default:
		return ".wav"
	}
}

// getAudioMimeType returns a MIME type for the given audio format.
func getAudioMimeType(format string) string {
	switch format {
	case "wav":
		return "audio/wav"
	case "mp3":
		return "audio/mpeg"
	case "aac":
		return "audio/aac"
	case "ogg":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	case "m4a":
		return "audio/mp4"
	case "aiff":
		return "audio/aiff"
	case "opus":
		return "audio/opus"
	default:
		return "audio/" + format
	}
}

// extractInputAudioData extracts audio data from an OpenAI input_audio content block.
// Format: {"type": "input_audio", "input_audio": {"data": "<base64>", "format": "wav"}}
func extractInputAudioData(block map[string]interface{}) (data string, format string, ok bool) {
	inputAudio, exists := block["input_audio"].(map[string]interface{})
	if !exists {
		// Also try snake_case variant "inputAudio" from SDK
		inputAudio, exists = block["inputAudio"].(map[string]interface{})
		if !exists {
			return "", "", false
		}
	}

	data, _ = inputAudio["data"].(string)
	format, _ = inputAudio["format"].(string)
	if data == "" {
		return "", "", false
	}
	if format == "" {
		format = "wav"
	}
	return data, format, true
}

// cleanupTempAudio removes any temp audio files we created.
func cleanupTempAudio() {
	pattern := filepath.Join(os.TempDir(), "llmproxy-audio-*")
	files, _ := filepath.Glob(pattern)
	for _, f := range files {
		os.Remove(f)
	}
}
