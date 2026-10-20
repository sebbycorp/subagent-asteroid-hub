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
}

// SessionInfo is one kagent session as the traffic generator sees it.
type SessionInfo struct {
	ID        string
	Name      string
	Agent     AgentRef
	CreatedAt time.Time
}
