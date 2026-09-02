package agent

// Confirmer asks a human to approve a pending write action before it
// executes. The CLI implementation prompts on stdin; a Slack
// implementation would post a message with Approve/Deny buttons and
// block on the response.
type Confirmer interface {
	// Confirm describes the pending action and returns true if approved.
	Confirm(toolName, argsPreview string) (bool, error)
}
