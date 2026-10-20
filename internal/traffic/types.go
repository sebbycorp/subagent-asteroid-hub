package traffic

import "time"

// AgentRef names a kagent Agent.
type AgentRef struct {
	Namespace string
	Name      string
}

// SessionInfo is one kagent session as the traffic generator sees it.
type SessionInfo struct {
	ID        string
	Name      string
	Agent     AgentRef
	CreatedAt time.Time
}
