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

type progressAgent struct{}

func (a *progressAgent) Run(ctx context.Context, req AgentRequest) (<-chan Event, error) {
	ch := make(chan Event, 4)
	ch <- Event{Type: EventStarted, SessionID: "thread-1"}
	ch <- Event{Type: EventTool, Text: "Bash: echo hello", SessionID: "thread-1"}
	ch <- Event{Type: EventText, Text: "reply", SessionID: "thread-1"}
	ch <- Event{Type: EventDone, SessionID: "thread-1"}
	close(ch)
	return ch, nil
}

type thinkingAgent struct{}

func (a *thinkingAgent) Run(ctx context.Context, req AgentRequest) (<-chan Event, error) {
	ch := make(chan Event, 4)
	ch <- Event{Type: EventStarted, SessionID: "thread-1"}
	ch <- Event{Type: EventThinking, Text: "先检查配置，再执行命令。", SessionID: "thread-1"}
	ch <- Event{Type: EventText, Text: "reply", SessionID: "thread-1"}
	ch <- Event{Type: EventDone, SessionID: "thread-1"}
	close(ch)
	return ch, nil
}

type usageAgent struct{}

func (a *usageAgent) Run(ctx context.Context, req AgentRequest) (<-chan Event, error) {
	ch := make(chan Event, 3)
	ch <- Event{Type: EventStarted, SessionID: "thread-1"}
	ch <- Event{Type: EventText, Text: "reply", SessionID: "thread-1"}
	ch <- Event{Type: EventDone, SessionID: "thread-1", Usage: &TokenUsage{
		UsedTokens:               3700,
		TotalTokens:              3700,
		InputTokens:              1200,
		CachedInputTokens:        900,
		CacheCreationInputTokens: 0,
		OutputTokens:             80,
		ContextWindow:            10000,
	}}
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
	for _, text := range []string{"/pwd", "/mode", "/model", "/display thinking", "/stop"} {
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

func TestFeishuSendTextMenuLabelsAreCommands(t *testing.T) {
	platform := &fakePlatform{}
	svc, err := New(Options{
		Agent:    &fakeAgent{},
		Platform: platform,
		DataDir:  t.TempDir(),
		Runtime: RuntimeInfo{
			WorkDir: "/tmp/project",
			Model:   "gpt-5",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	labels := []string{
		"新建会话",
		"会话列表",
		"当前会话",
		"停止执行",
		"当前状态",
		"工作目录",
		"模式",
		"模型",
		"帮助",
		"显示思考（默认）",
		"关闭思考",
		"极简模式",
	}
	for _, label := range labels {
		if !svc.handleCommand(context.Background(), Message{SessionKey: "k1", Text: label}) {
			t.Fatalf("menu label %q not handled", label)
		}
	}
	joined := strings.Join(platform.sent, "\n")
	for _, want := range []string{"新的 Codex 会话", "暂无已保存会话", "当前聊天还没有绑定", "没有正在执行", "/tmp/project", "gpt-5", "显示思考", "只看结果", "极简模式"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %#v", want, platform.sent)
		}
	}
}

func TestDefaultDisplayModeSendsProgress(t *testing.T) {
	platform := &fakePlatform{}
	svc, err := New(Options{
		Agent:         &progressAgent{},
		Platform:      platform,
		DataDir:       t.TempDir(),
		QueueMessages: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	svc.runTurn(context.Background(), Message{SessionKey: "k1", Text: "hello"})

	if len(platform.sent) != 2 {
		t.Fatalf("sent=%#v", platform.sent)
	}
	if !strings.Contains(platform.sent[0], "执行中") || !strings.Contains(platform.sent[0], "Bash: echo hello") {
		t.Fatalf("progress not sent: %#v", platform.sent)
	}
	if platform.sent[1] != "reply" {
		t.Fatalf("final reply=%#v", platform.sent)
	}
}

func TestDefaultDisplayModeSendsThinking(t *testing.T) {
	platform := &fakePlatform{}
	svc, err := New(Options{
		Agent:         &thinkingAgent{},
		Platform:      platform,
		DataDir:       t.TempDir(),
		QueueMessages: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	svc.runTurn(context.Background(), Message{SessionKey: "k1", Text: "hello"})

	if len(platform.sent) != 2 {
		t.Fatalf("sent=%#v", platform.sent)
	}
	if !strings.Contains(platform.sent[0], "思考中") || !strings.Contains(platform.sent[0], "先检查配置") {
		t.Fatalf("thinking not sent: %#v", platform.sent)
	}
	if platform.sent[1] != "reply" {
		t.Fatalf("final reply=%#v", platform.sent)
	}
}

func TestFinalDisplayModeSuppressesThinking(t *testing.T) {
	platform := &fakePlatform{}
	svc, err := New(Options{
		Agent:         &thinkingAgent{},
		Platform:      platform,
		DataDir:       t.TempDir(),
		QueueMessages: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.setDisplayMode(displayFinal)

	svc.runTurn(context.Background(), Message{SessionKey: "k1", Text: "hello"})

	if len(platform.sent) != 1 || platform.sent[0] != "reply" {
		t.Fatalf("sent=%#v", platform.sent)
	}
}

func TestRunTurnAppendsReplyFooter(t *testing.T) {
	platform := &fakePlatform{}
	svc, err := New(Options{
		Agent:         &usageAgent{},
		Platform:      platform,
		DataDir:       t.TempDir(),
		QueueMessages: false,
		Runtime: RuntimeInfo{
			WorkDir:         "/tmp/project",
			Model:           "gpt-5.5",
			ReasoningEffort: "xhigh",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	svc.runTurn(context.Background(), Message{SessionKey: "k1", Text: "hello"})

	if len(platform.sent) != 1 {
		t.Fatalf("sent=%#v", platform.sent)
	}
	for _, want := range []string{"reply", "---", "gpt-5.5", "effort:xhigh", "out 80", "in 1.2k cw 0 cr 900", "ctx 37%", "/tmp/project"} {
		if !strings.Contains(platform.sent[0], want) {
			t.Fatalf("missing %q in %q", want, platform.sent[0])
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
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type=%s", got)
		}
		var event map[string]any
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
		if len(event) != 2 || event["姓名"] != "Alice" || event["是否成功"] != true {
			t.Fatalf("event=%#v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote usage report")
	}
}

func TestUsageTrackerReportsWorkflowDailyUsage(t *testing.T) {
	type receivedEvent struct {
		auth  string
		event map[string]any
	}
	received := make(chan receivedEvent, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var event map[string]any
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("decode event: %v", err)
		}
		received <- receivedEvent{auth: r.Header.Get("Authorization"), event: event}
	}))
	defer server.Close()

	tracker, err := OpenUsageTracker(t.TempDir(), UsageOptions{
		WorkflowReportURL: server.URL,
		WorkflowToken:     "secret-token",
		WorkflowProject:   "sy-feishu-connect",
	})
	if err != nil {
		t.Fatal(err)
	}
	eventTime := time.Date(2026, 7, 10, 9, 0, 0, 0, time.Local)
	for i := 0; i < 2; i++ {
		if err := tracker.Record(UsageEvent{
			Time:             eventTime.Add(time.Duration(i) * time.Hour),
			SessionKey:       "s1",
			UserID:           "ou_user",
			FeishuUserName:   "段成亮",
			FeishuEmployeeNo: "sy4044",
			Kind:             "task",
			Success:          true,
			TextChars:        3,
		}); err != nil {
			t.Fatal(err)
		}
	}

	counts := map[int]bool{}
	for i := 0; i < 2; i++ {
		select {
		case got := <-received:
			if got.auth != "Bearer secret-token" {
				t.Fatalf("auth=%q", got.auth)
			}
			if got.event["用户姓名"] != "段成亮" || got.event["飞书工号"] != "sy4044" || got.event["日期"] != "2026-07-10" || got.event["项目"] != "sy-feishu-connect" {
				t.Fatalf("event=%#v", got.event)
			}
			counts[int(got.event["当日使用次数"].(float64))] = true
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for workflow usage report")
		}
	}
	if !counts[1] || !counts[2] {
		t.Fatalf("counts=%#v", counts)
	}
}

func TestUsageTrackerFormatsFeishuWebhookReports(t *testing.T) {
	payload, err := marshalRemoteUsagePayload("https://open.feishu.cn/open-apis/bot/v2/hook/test-token", RemoteUsageEvent{
		Name:    "张三",
		Success: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if event["msg_type"] != "text" {
		t.Fatalf("event=%#v", event)
	}
	content, ok := event["content"].(map[string]any)
	if !ok {
		t.Fatalf("content=%#v", event["content"])
	}
	text, _ := content["text"].(string)
	if !strings.Contains(text, "姓名：张三") || !strings.Contains(text, "是否成功：是") {
		t.Fatalf("text=%q", text)
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
