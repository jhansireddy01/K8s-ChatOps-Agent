package llm

import "encoding/json"

// Message is one turn in the conversation, following the OpenAI/Ollama
// chat schema (role: system|user|assistant|tool).
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // set on role=tool messages
	Name       string     `json:"name,omitempty"`         // set on role=tool messages
}

// ToolCall is a single function invocation the model asked for.
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type"` // always "function"
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolDefinition describes one callable tool to the model, JSON-schema style.
type ToolDefinition struct {
	Type     string       `json:"type"` // always "function"
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  ParamsSpec `json:"parameters"`
}

type ParamsSpec struct {
	Type       string              `json:"type"` // "object"
	Properties map[string]PropSpec `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type PropSpec struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// ChatRequest matches Ollama's /api/chat request body.
type ChatRequest struct {
	Model    string           `json:"model"`
	Messages []Message        `json:"messages"`
	Tools    []ToolDefinition `json:"tools,omitempty"`
	Stream   bool             `json:"stream"`
}

// ChatResponse matches Ollama's /api/chat response body (stream=false).
type ChatResponse struct {
	Model      string  `json:"model"`
	Message    Message `json:"message"`
	Done       bool    `json:"done"`
	DoneReason string  `json:"done_reason,omitempty"`
}
