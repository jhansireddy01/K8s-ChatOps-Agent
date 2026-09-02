// Package tools defines the concrete K8s/ArgoCD actions the agent can
// take, and a registry to look them up by name. Every tool declares
// IsWrite() so the agent loop knows which ones must be gated behind
// user confirmation - this is the single source of truth for the
// "read-only by default" safety rule, not something re-decided ad hoc
// in the agent or the LLM prompt.
package tools

import (
	"encoding/json"
	"fmt"

	"github.com/yourorg/chatops-agent/internal/llm"
)

// Tool is one callable action exposed to the LLM.
type Tool interface {
	Name() string
	Description() string
	Params() llm.ParamsSpec
	IsWrite() bool
	Execute(argsJSON json.RawMessage) (string, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.tools[t.Name()] = t
	}
	return r
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Definitions returns the tool list in the format the LLM client sends
// to the model.
func (r *Registry) Definitions() []llm.ToolDefinition {
	defs := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, llm.ToolDefinition{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Params(),
			},
		})
	}
	return defs
}

func (r *Registry) Execute(name string, argsJSON json.RawMessage) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return t.Execute(argsJSON)
}

func (r *Registry) IsWrite(name string) bool {
	t, ok := r.Get(name)
	return ok && t.IsWrite()
}
