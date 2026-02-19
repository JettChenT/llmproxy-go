package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
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

	var b strings.Builder

	// Request section: method, URL, headers, body
	b.WriteString("Input:\n")
	b.WriteString(fmt.Sprintf("%s %s HTTP/1.1\n", m.selected.Method, m.selected.Path))
	if m.selected.Host != "" {
		b.WriteString(fmt.Sprintf("Host: %s\n", m.selected.Host))
	}
	if len(m.selected.RequestHeaders) > 0 {
		headerKeys := make([]string, 0, len(m.selected.RequestHeaders))
		for k := range m.selected.RequestHeaders {
			if k == "Host" {
				continue
			}
			headerKeys = append(headerKeys, k)
		}
		sort.Strings(headerKeys)
		for _, k := range headerKeys {
			values := m.selected.RequestHeaders[k]
			headerValue := strings.Join(values, ", ")
			if strings.ToLower(k) == "authorization" && len(headerValue) > 30 {
				headerValue = headerValue[:20] + "..." + headerValue[len(headerValue)-10:]
			}
			b.WriteString(fmt.Sprintf("%s: %s\n", k, headerValue))
		}
	}
	b.WriteString("\n")

	inputText, _, inputErr := rawBodyCopyText(m.selected.RequestBody, "input")
	if inputErr != nil {
		m.copyMessage = fmt.Sprintf("✗ %s", inputErr.Error())
		m.copyMessageTime = time.Now()
		return
	}
	b.WriteString(inputText)

	// Response section: status, headers, body
	b.WriteString("\n\nOutput:\n")
	if m.selected.StatusCode > 0 {
		statusText := http.StatusText(m.selected.StatusCode)
		b.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\n", m.selected.StatusCode, statusText))
	}
	if len(m.selected.ResponseHeaders) > 0 {
		headerKeys := make([]string, 0, len(m.selected.ResponseHeaders))
		for k := range m.selected.ResponseHeaders {
			headerKeys = append(headerKeys, k)
		}
		sort.Strings(headerKeys)
		for _, k := range headerKeys {
			values := m.selected.ResponseHeaders[k]
			b.WriteString(fmt.Sprintf("%s: %s\n", k, strings.Join(values, ", ")))
		}
	}
	b.WriteString("\n")

	outputText, _, outputErr := outputCopyText(m.selected)
	if outputErr != nil {
		m.copyMessage = fmt.Sprintf("✗ %s", outputErr.Error())
		m.copyMessageTime = time.Now()
		return
	}
	b.WriteString(outputText)

	if err := clipboard.WriteAll(b.String()); err != nil {
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

	// Truncate long base64 strings (e.g. images) to match the raw view display
	body = truncateLongBase64Strings(body)

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
