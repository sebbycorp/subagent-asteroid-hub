package traffic

import "time"

// AgentRef names a kagent Agent.
type AgentRef struct {
	Namespace string
	Name      string
}

// ChatResult is what one chat turn produced.
type ChatResult struct {
	Reply  string // the agent's reply text
	TaskID string // the A2A task of the turn (a checkpoint's head)
	// Approval is set when the turn paused for a human to approve tool calls
	// (human in the loop). The agent is suspended until someone decides.
	Approval *Approval
}

// Approval is a paused turn waiting for a human decision on tool calls.
type Approval struct {
	TaskID string
	Tools  []ApprovalTool
}

// ApprovalTool is one tool call awaiting approval.
type ApprovalTool struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// SessionInfo is one kagent session as the traffic generator sees it.
type SessionInfo struct {
	ID        string
	Name      string
	Agent     AgentRef
	CreatedAt time.Time
}
