package domain

import "time"

// Activity represents an audit log entry
type Activity struct {
	ID       string                 `json:"id"`
	Time     time.Time              `json:"time"`
	User     string                 `json:"user"`
	Role     string                 `json:"role,omitempty"`
	Action   string                 `json:"action"`
	Target   string                 `json:"target"`
	Detail   map[string]interface{} `json:"detail,omitempty"`
	RemoteIP string                 `json:"remoteIp,omitempty"`
}
