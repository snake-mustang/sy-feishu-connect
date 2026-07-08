package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

type eventErrorAgent struct{}

func (a *eventErrorAgent) Run(ctx context.Context, req AgentRequest) (<-chan Event, error) {
	ch := make(chan Event, 1)
	ch <- Event{Type: EventError, Err: errors.New("api key login required")}
	close(ch)
	return ch, nil
}

type fakePlatform struct {
	mu       sync.Mutex
	handler  func(context.Context, Message)
	sent     []string
	profiles map[string]UserProfile
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

func (p *fakePlatform) ResolveUser(ctx context.Context, userID string) (UserProfile, error) {
	if p.profiles != nil {
		if profile, ok := p.profiles[userID]; ok {
			if profile.ID == "" {
				profile.ID = userID
			}
			return profile, nil
		}
	}
	return UserProfile{ID: userID}, nil
}

func TestRunTurnPersistsSession(t *testing.T) {
	agent := &fakeAgent{}
	platform := &fakePlatform{}
	dataDir := t.TempDir()
	svc, err := New(Options{
		Agent:         agent,
		Platform:      platform,
		DataDir:       dataDir,
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
	summary, err := os.ReadFile(filepath.Join(dataDir, "usage_summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(summary), `"tasks": 1`) {
		t.Fatalf("usage summary=%s", summary)
	}
}

func TestRunTurnErrorEventDoesNotSendEmptySuccess(t *testing.T) {
	platform := &fakePlatform{}
	svc, err := New(Options{
		Agent:         &eventErrorAgent{},
		Platform:      platform,
		DataDir:       t.TempDir(),
		QueueMessages: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.runTurn(context.Background(), Message{SessionKey: "k1", Text: "hello"})
	if len(platform.sent) != 1 {
		t.Fatalf("sent=%#v", platform.sent)
	}
	if !strings.Contains(platform.sent[0], "Codex 执行失败") || strings.Contains(platform.sent[0], "没有返回文本") {
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

func TestMenuMessageUsesLatestUserSession(t *testing.T) {
	svc, err := New(Options{
		Agent:    &fakeAgent{},
		Platform: &fakePlatform{},
		DataDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	normal := svc.bindMessageSession(Message{SessionKey: "feishu:chat-a:ou_user", ChatID: "chat-a", ChatType: "p2p", UserID: "ou_user", Text: "hello"})
	if normal.SessionKey != "feishu:chat-a:ou_user" {
		t.Fatalf("normal session=%q", normal.SessionKey)
	}
	menu := svc.bindMessageSession(Message{SessionKey: "feishu:menu:ou_user", ChatType: "menu", UserID: "ou_user", Text: "/status"})
	if menu.SessionKey != "feishu:chat-a:ou_user" {
		t.Fatalf("menu session=%q", menu.SessionKey)
	}
}

func TestRuntimeCommands(t *testing.T) {
	platform := &fakePlatform{}
	svc, err := New(Options{
		Agent:    &fakeAgent{},
		Platform: platform,
		DataDir:  t.TempDir(),
		Runtime: RuntimeInfo{
			WorkDir:         "/tmp/project",
			Mode:            "auto-edit",
			Model:           "gpt-5",
			ReasoningEffort: "high",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"/pwd", "/mode", "/model", "/display full", "/stop"} {
		if !svc.handleCommand(context.Background(), Message{SessionKey: "k1", Text: text}) {
			t.Fatalf("command %q not handled", text)
		}
	}
	joined := strings.Join(platform.sent, "\n")
	for _, want := range []string{"/tmp/project", "auto-edit", "gpt-5", "显示思考", "没有正在执行"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %#v", want, platform.sent)
		}
	}
}

func TestStatsAndWhoamiCommands(t *testing.T) {
	platform := &fakePlatform{profiles: map[string]UserProfile{
		"ou_user": {Name: "Alice", EmployeeNo: "E001"},
	}}
	svc, err := New(Options{
		Agent:    &fakeAgent{},
		Platform: platform,
		DataDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.runTurn(context.Background(), Message{SessionKey: "k1", ChatID: "chat-a", ChatType: "group", UserID: "ou_user", Text: "hello"})
	if !svc.handleCommand(context.Background(), Message{SessionKey: "k1", ChatID: "chat-a", ChatType: "group", UserID: "ou_user", Text: "/stats"}) {
		t.Fatal("stats command not handled")
	}
	if !svc.handleCommand(context.Background(), Message{SessionKey: "k1", ChatID: "chat-a", ChatType: "group", UserID: "ou_user", Text: "/whoami"}) {
		t.Fatal("whoami command not handled")
	}
	platform.mu.Lock()
	defer platform.mu.Unlock()
	joined := strings.Join(platform.sent, "\n")
	if !strings.Contains(joined, "使用统计") || !strings.Contains(joined, "ou_user") || !strings.Contains(joined, "Alice") || !strings.Contains(joined, "E001") || !strings.Contains(joined, "你的飞书用户标识") {
		t.Fatalf("sent=%#v", platform.sent)
	}
}

func TestUsageTrackerReportsRemoteEvents(t *testing.T) {
	received := make(chan UsageEvent, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type=%s", got)
		}
		var event UsageEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("decode event: %v", err)
		}
		received <- event
	}))
	defer server.Close()

	tracker, err := OpenUsageTracker(t.TempDir(), UsageOptions{
		OperatorName: "Alice",
		EmployeeID:   "E001",
		ReportURL:    server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Record(UsageEvent{SessionKey: "s1", UserID: "ou_user", Kind: "task", Success: true, TextChars: 3}); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-received:
		if event.OperatorName != "Alice" || event.EmployeeID != "E001" || event.UserID != "ou_user" {
			t.Fatalf("event=%#v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote usage report")
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
