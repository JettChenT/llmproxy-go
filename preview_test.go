package main

import "testing"

func TestExtractRequestPreviewSnippetOpenAISystemFirst(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4o",
		"messages":[
			{"role":"user","content":"Summarize this changelog."},
			{"role":"system","content":"You are a release-note classifier."}
		]
	}`)

	got := extractRequestPreviewSnippet("/v1/chat/completions", body)
	want := "You are a release-note classifier."
	if got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestExtractRequestPreviewSnippetOpenAIFirstMessageFallback(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4o",
		"messages":[
			{"role":"user","content":"\n\t"},
			{"role":"user","content":" classify this support ticket please "}
		]
	}`)

	got := extractRequestPreviewSnippet("/v1/chat/completions", body)
	want := "classify this support ticket please"
	if got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestExtractRequestPreviewSnippetOpenAIArrayContent(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4o",
		"messages":[
			{
				"role":"user",
				"content":[
					{"type":"text","text":"Route to billing workflow"},
					{"type":"image_url","image_url":{"url":"https://example.com/invoice.png"}}
				]
			}
		]
	}`)

	got := extractRequestPreviewSnippet("/v1/chat/completions", body)
	want := "Route to billing workflow"
	if got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestExtractRequestPreviewSnippetAnthropicSystemFirst(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4",
		"max_tokens":512,
		"system":[{"type":"text","text":"You are a SQL query explainer."}],
		"messages":[
			{"role":"user","content":"Explain this query plan."}
		]
	}`)

	got := extractRequestPreviewSnippet("/v1/messages", body)
	want := "You are a SQL query explainer."
	if got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestExtractRequestPreviewSnippetAnthropicFirstMessageFallback(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4",
		"max_tokens":512,
		"messages":[
			{"role":"user","content":"\n"},
			{"role":"user","content":"Draft onboarding email copy"}
		]
	}`)

	got := extractRequestPreviewSnippet("/v1/messages", body)
	want := "Draft onboarding email copy"
	if got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestExtractRequestPreviewSnippetInvalidBody(t *testing.T) {
	got := extractRequestPreviewSnippet("/v1/chat/completions", []byte(`{not-json`))
	if got != "" {
		t.Fatalf("preview = %q, want empty", got)
	}
}

func TestGetRequestPreviewSnippetUsesCache(t *testing.T) {
	m := initialModel(":8080", "https://api.openai.com", "", "")
	req := &LLMRequest{
		ID:   99,
		Path: "/v1/chat/completions",
		RequestBody: []byte(`{
			"model":"gpt-4o",
			"messages":[{"role":"user","content":"first snippet"}]
		}`),
	}

	first := m.getRequestPreviewSnippet(req)
	req.RequestBody = []byte(`{
		"model":"gpt-4o",
		"messages":[{"role":"user","content":"second snippet"}]
	}`)
	second := m.getRequestPreviewSnippet(req)

	if first != "first snippet" {
		t.Fatalf("first preview = %q, want %q", first, "first snippet")
	}
	if second != first {
		t.Fatalf("cached preview = %q, want %q", second, first)
	}
}
