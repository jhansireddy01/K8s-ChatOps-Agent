// Package agent implements the tool-calling loop: it is the "brain" that
// turns a natural-language question into a sequence of tool calls and a
// final natural-language answer. It has no knowledge of Slack or the CLI
// - those just implement Confirmer and drive Agent.Ask in a loop.
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yourorg/chatops-agent/internal/llm"
	"github.com/yourorg/chatops-agent/internal/tools"
)

const systemPrompt = `You are a ChatOps assistant for Kubernetes and ArgoCD.
You help engineers answer operational questions and, when explicitly asked,
take actions - by calling the tools available to you. Rules:

1. Always prefer read-only tools to answer questions before considering a
   write action.
2. Never call a write tool (scaling a deployment, syncing an application)
   unless the user's request clearly asks for that action to be taken.
3. When you call a tool, wait for its result before answering - do not
   guess at cluster state.
4. Keep answers concise and concrete: name the actual apps/pods/namespaces
   involved, not generic advice.
5. If a tool call fails, explain the failure plainly; do not retry blindly
   more than once.`

const maxToolIterations = 8

type Agent struct {
	llm       *llm.Client
	registry  *tools.Registry
	confirmer Confirmer
	history   []llm.Message
}

func New(llmClient *llm.Client, registry *tools.Registry, confirmer Confirmer) *Agent {
	return &Agent{
		llm:       llmClient,
		registry:  registry,
		confirmer: confirmer,
		history:   []llm.Message{{Role: "system", Content: systemPrompt}},
	}
}

// Ask sends a user message, drives the tool-calling loop to completion,
// and returns the model's final natural-language answer. The full
// history (including tool calls/results) is retained for follow-up
// questions in the same session.
func (a *Agent) Ask(ctx context.Context, userMessage string) (string, error) {
	a.history = append(a.history, llm.Message{Role: "user", Content: userMessage})

	for i := 0; i < maxToolIterations; i++ {
		msg, err := a.llm.Chat(ctx, a.history, a.registry.Definitions())
		if err != nil {
			return "", fmt.Errorf("llm call failed: %w", err)
		}
		a.history = append(a.history, *msg)

		if len(msg.ToolCalls) == 0 {
			// No more tools requested - this is the final answer.
			return msg.Content, nil
		}

		for _, call := range msg.ToolCalls {
			result, err := a.handleToolCall(call)
			if err != nil {
				result = fmt.Sprintf("ERROR: %v", err)
			}
			a.history = append(a.history, llm.Message{
				Role:       "tool",
				Name:       call.Function.Name,
				ToolCallID: call.ID,
				Content:    result,
			})
		}
	}

	return "", fmt.Errorf("stopped after %d tool-call iterations without a final answer - possible loop", maxToolIterations)
}

func (a *Agent) handleToolCall(call llm.ToolCall) (string, error) {
	name := call.Function.Name

	tool, ok := a.registry.Get(name)
	if !ok {
		return "", fmt.Errorf("model requested unknown tool %q", name)
	}

	if tool.IsWrite() {
		approved, err := a.confirmer.Confirm(name, previewArgs(call.Function.Arguments))
		if err != nil {
			return "", fmt.Errorf("confirmation prompt failed: %w", err)
		}
		if !approved {
			return "Action declined by user; no changes were made.", nil
		}
	}

	return a.registry.Execute(name, call.Function.Arguments)
}

func previewArgs(raw json.RawMessage) string {
	var pretty map[string]any
	if err := json.Unmarshal(raw, &pretty); err != nil {
		return string(raw)
	}
	b, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(b)
}
