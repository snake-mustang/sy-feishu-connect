package bridge

import (
	"context"
)

type Message struct {
	SessionKey string
	MessageID  string
	ChatID     string
	ChatType   string
	UserID     string
	Text       string
	ReplyCtx   any
}

type RuntimeInfo struct {
	WorkDir         string
	Mode            string
	Model           string
	ReasoningEffort string
}

type UserProfile struct {
	ID         string
	Name       string
	EmployeeNo string
}

type Platform interface {
	Start(context.Context, func(context.Context, Message)) error
	Send(context.Context, any, string) error
	ReactWorking(context.Context, any) error
	ReactDone(context.Context, any) error
}

type UserResolver interface {
	ResolveUser(context.Context, string) (UserProfile, error)
}

type Agent interface {
	Run(context.Context, AgentRequest) (<-chan Event, error)
}

type AgentRequest struct {
	SessionID string
	Prompt    string
}

type EventType string

const (
	EventStarted  EventType = "started"
	EventText     EventType = "text"
	EventThinking EventType = "thinking"
	EventTool     EventType = "tool"
	EventError    EventType = "error"
	EventDone     EventType = "done"
)

type Event struct {
	Type      EventType
	Text      string
	SessionID string
	Err       error
}
