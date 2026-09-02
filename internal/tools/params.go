package tools

import "github.com/yourorg/chatops-agent/internal/llm"

// Aliases so tool files read a little less verbosely.
type paramsSpecType = llm.ParamsSpec
type propSpecType = llm.PropSpec

func newParams(props map[string]propSpecType, required []string) paramsSpecType {
	return paramsSpecType{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}
