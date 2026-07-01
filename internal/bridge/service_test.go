package bridge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type fakeAgent struct {
	mu       sync.Mutex
	requests []AgentRequest
}

func (a *fakeAgent) Run(ctx context.Context, req AgentRequest) (<-chan Event, error) {
	a.mu.Lock()
	a.requests = append(a.requests, req)
	a.mu.Unlock()
	ch := make(chan Event, 3)
	ch <- Event{Type: EventStarted, SessionID: "thread-1"}
	ch <- Event{Type: EventText, Text: "reply"}
	ch <- Event{Type: EventDone, SessionID: "thread-1"}
	close(ch)
	return ch, nil
}

type failingAgent struct{}

func (a *failingAgent) Run(ctx context.Context, req AgentRequest) (<-chan Event, error) {
	return nil, errors.New("boom")
}

type fakePlatform struct {
	mu      sync.Mutex
	handler func(context.Context, Message)
	sent    []string
}

func (p *fakePlatform) Start(ctx context.Context, h func(context.Context, Message)) error {
	p.handler = h
	return nil
}

func (p *fakePlatform) Send(ctx context.Context, replyCtx any, text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, text)
	return nil
}

func (p *fakePlatform) ReactWorking(context.Context, any) error { return nil }
func (p *fakePlatform) ReactDone(context.Context, any) error    { return nil }

func TestRunTurnPersistsSession(t *testing.T) {
	agent := &fakeAgent{}
	platform := &fakePlatform{}
	svc, err := New(Options{
		Agent:         agent,
		Platform:      platform,
		DataDir:       t.TempDir(),
		QueueMessages: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{SessionKey: "k1", Text: "hello"}
	svc.runTurn(context.Background(), msg)
	if got := svc.store.Get("k1").ThreadID; got != "thread-1" {
		t.Fatalf("thread=%q", got)
	}
	if len(platform.sent) != 1 || platform.sent[0] != "reply" {
		t.Fatalf("sent=%#v", platform.sent)
	}
}

func TestStatusCommand(t *testing.T) {
	platform := &fakePlatform{}
	svc, err := New(Options{
		Agent:    &fakeAgent{},
		Platform: platform,
		DataDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetThread("k1", "thread-x"); err != nil {
		t.Fatal(err)
	}
	if !svc.handleCommand(context.Background(), Message{SessionKey: "k1", Text: "/status"}) {
		t.Fatal("command not handled")
	}
	if len(platform.sent) != 1 || !strings.Contains(platform.sent[0], "thread-x") {
		t.Fatalf("sent=%#v", platform.sent)
	}
}

func TestStorePersists(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetThread("k", "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(filepath.Join(dir, "missing-child")); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Get("k").ThreadID; got != "t" {
		t.Fatalf("got %q", got)
	}
}
