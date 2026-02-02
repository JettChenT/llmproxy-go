package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/atotto/clipboard"
)

func (m *model) copyActiveTab() {
	if m.selected == nil {
		m.copyMessage = "✗ No request selected"
		m.copyMessageTime = time.Now()
		return
	}

	text, label, err := m.getCopyText()
	if err != nil {
		m.copyMessage = fmt.Sprintf("✗ %s", err.Error())
		m.copyMessageTime = time.Now()
		return
	}

	if err := clipboard.WriteAll(text); err != nil {
		m.copyMessage = fmt.Sprintf("✗ Clipboard error: %v", err)
		m.copyMessageTime = time.Now()
		return
	}

	m.copyMessage = fmt.Sprintf("Copied %s", label)
	m.copyMessageTime = time.Now()
}

func (m *model) copyInputOutput() {
	if m.selected == nil {
		m.copyMessage = "✗ No request selected"
		m.copyMessageTime = time.Now()
		return
	}

	inputText, _, inputErr := rawBodyCopyText(m.selected.RequestBody, "input")
	outputText, _, outputErr := outputCopyText(m.selected)
	if inputErr != nil {
		m.copyMessage = fmt.Sprintf("✗ %s", inputErr.Error())
		m.copyMessageTime = time.Now()
		return
	}
	if outputErr != nil {
		m.copyMessage = fmt.Sprintf("✗ %s", outputErr.Error())
		m.copyMessageTime = time.Now()
		return
	}

	combined := fmt.Sprintf("Input:\n%s\n\nOutput:\n%s", inputText, outputText)
	if err := clipboard.WriteAll(combined); err != nil {
		m.copyMessage = fmt.Sprintf("✗ Clipboard error: %v", err)
		m.copyMessageTime = time.Now()
		return
	}

	m.copyMessage = "Copied input+output"
	m.copyMessageTime = time.Now()
}

func (m *model) getCopyText() (string, string, error) {
	switch m.activeTab {
	case TabOutput:
		return outputCopyText(m.selected)
	case TabRawInput:
		return rawBodyCopyText(m.selected.RequestBody, "raw input")
	case TabRawOutput:
		return rawBodyCopyText(m.selected.ResponseBody, "raw output")
	default:
		return "", "", fmt.Errorf("nothing to copy")
	}
}

func rawBodyCopyText(body []byte, label string) (string, string, error) {
	if len(body) == 0 {
		return "", label, fmt.Errorf("no %s", label)
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err == nil {
		return pretty.String(), label, nil
	}

	return string(body), label, nil
}

func outputCopyText(req *LLMRequest) (string, string, error) {
	if req == nil {
		return "", "output", fmt.Errorf("no request selected")
	}
	if len(req.ResponseBody) == 0 {
		if req.Status == StatusPending {
			return "", "output", fmt.Errorf("response pending")
		}
		return "", "output", fmt.Errorf("no output body")
	}
	return rawBodyCopyText(req.ResponseBody, "output")
}
