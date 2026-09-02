// Package llm wraps a local Ollama server and speaks its tool-calling
// dialect (OpenAI-function-style). Only models that support tool-calling
// work here - e.g. qwen2.5, llama3.1, mistral-nemo, firefunction-v2.
// Plain chat models will just ignore the "tools" field and never call
// anything, so the agent will silently degrade to Q&A - check
// `ollama show <model>` for "tools" in the capabilities list if a model
// isn't invoking any tools.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

func NewClient(baseURL, model string) *Client {
	// CPU-only inference over a large tool-calling context can take minutes,
	// and Ollama is non-streaming here (no headers until the full response is
	// ready), so the client timeout must cover the entire generation. Default
	// generously; override with OLLAMA_TIMEOUT_SECONDS.
	timeout := 600 * time.Second
	if v := os.Getenv("OLLAMA_TIMEOUT_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			timeout = time.Duration(secs) * time.Second
		}
	}
	return &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: timeout},
	}
}

// Chat sends the full message history plus tool definitions and returns
// the model's next message (which may itself contain tool calls).
func (c *Client) Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error) {
	reqBody := ChatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling ollama at %s (is `ollama serve` running?): %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading ollama response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(raw))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(raw, &chatResp); err != nil {
		return nil, fmt.Errorf("decoding ollama response: %w (body: %s)", err, string(raw))
	}

	return &chatResp.Message, nil
}
