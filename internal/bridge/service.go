package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Agent         Agent
	Platform      Platform
	DataDir       string
	QueueMessages bool
	Usage         UsageOptions
}

type Service struct {
	agent         Agent
	platform      Platform
	store         *Store
	usage         *UsageTracker
	queueMessages bool
	mu            sync.Mutex
	sessions      map[string]*sessionWorker
}

type sessionWorker struct {
	key    string
	svc    *Service
	ch     chan Message
	closed chan struct{}
}

func New(opts Options) (*Service, error) {
	if opts.Agent == nil {
		return nil, fmt.Errorf("bridge: Agent is required")
	}
	if opts.Platform == nil {
		return nil, fmt.Errorf("bridge: Platform is required")
	}
	store, err := OpenStore(opts.DataDir)
	if err != nil {
		return nil, err
	}
	usage, err := OpenUsageTracker(opts.DataDir, opts.Usage)
	if err != nil {
		return nil, err
	}
	return &Service{
		agent:         opts.Agent,
		platform:      opts.Platform,
		store:         store,
		usage:         usage,
		queueMessages: opts.QueueMessages,
		sessions:      map[string]*sessionWorker{},
	}, nil
}

func (s *Service) Start(ctx context.Context) error {
	return s.platform.Start(ctx, s.Receive)
}

func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, worker := range s.sessions {
		close(worker.ch)
	}
	return nil
}

func (s *Service) Receive(ctx context.Context, msg Message) {
	if strings.TrimSpace(msg.Text) == "" {
		return
	}
	if s.handleCommand(ctx, msg) {
		return
	}
	if !s.queueMessages {
		go s.runTurn(ctx, msg)
		return
	}
	worker := s.worker(msg.SessionKey)
	select {
	case worker.ch <- msg:
	default:
		_ = s.platform.Send(ctx, msg.ReplyCtx, "当前会话排队消息过多，请稍后再试。")
		if err := s.recordUsage(ctx, UsageEvent{
			Time:       time.Now(),
			SessionKey: msg.SessionKey,
			MessageID:  msg.MessageID,
			ChatID:     msg.ChatID,
			ChatType:   msg.ChatType,
			UserID:     msg.UserID,
			Kind:       "task",
			Success:    false,
			TextChars:  len([]rune(msg.Text)),
			Error:      "queue full",
		}); err != nil {
			slog.Warn("bridge: record queue usage failed", "error", err)
		}
	}
}

func (s *Service) worker(key string) *sessionWorker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w := s.sessions[key]; w != nil {
		return w
	}
	w := &sessionWorker{
		key:    key,
		svc:    s,
		ch:     make(chan Message, 16),
		closed: make(chan struct{}),
	}
	s.sessions[key] = w
	go w.loop()
	return w
}

func (w *sessionWorker) loop() {
	defer close(w.closed)
	for msg := range w.ch {
		w.svc.runTurn(context.Background(), msg)
	}
}

func (s *Service) runTurn(ctx context.Context, msg Message) {
	start := time.Now()
	success := false
	replyChars := 0
	errText := ""
	defer func() {
		if err := s.recordUsage(ctx, UsageEvent{
			Time:       start,
			SessionKey: msg.SessionKey,
			MessageID:  msg.MessageID,
			ChatID:     msg.ChatID,
			ChatType:   msg.ChatType,
			UserID:     msg.UserID,
			Kind:       "task",
			Success:    success,
			DurationMS: time.Since(start).Milliseconds(),
			TextChars:  len([]rune(msg.Text)),
			ReplyChars: replyChars,
			Error:      errText,
		}); err != nil {
			slog.Warn("bridge: record usage failed", "error", err)
		}
	}()

	_ = s.platform.ReactWorking(ctx, msg.ReplyCtx)
	state := s.store.Get(msg.SessionKey)
	events, err := s.agent.Run(ctx, AgentRequest{SessionID: state.ThreadID, Prompt: msg.Text})
	if err != nil {
		errText = err.Error()
		_ = s.platform.Send(ctx, msg.ReplyCtx, "Codex 启动失败: "+err.Error())
		return
	}

	var finalParts []string
	var lastTool time.Time
	for event := range events {
		if event.SessionID != "" {
			if err := s.store.SetThread(msg.SessionKey, event.SessionID); err != nil {
				slog.Warn("bridge: save session failed", "session_key", msg.SessionKey, "error", err)
			}
		}
		switch event.Type {
		case EventTool:
			if time.Since(lastTool) > 5*time.Second && strings.TrimSpace(event.Text) != "" {
				lastTool = time.Now()
				slog.Info("codex progress", "session_key", msg.SessionKey, "event", event.Text)
			}
		case EventText:
			if strings.TrimSpace(event.Text) != "" {
				finalParts = append(finalParts, event.Text)
			}
		case EventError:
			if event.Err != nil {
				errText = event.Err.Error()
				_ = s.platform.Send(ctx, msg.ReplyCtx, "Codex 执行失败: "+event.Err.Error())
			}
		}
	}
	final := strings.TrimSpace(strings.Join(finalParts, "\n\n"))
	if final == "" && errText != "" {
		return
	}
	if final == "" {
		final = "Codex 已完成，但没有返回文本。"
	}
	replyChars = len([]rune(final))
	if err := s.platform.Send(ctx, msg.ReplyCtx, final); err != nil {
		errText = err.Error()
		slog.Error("bridge: send reply failed", "error", err)
		return
	}
	_ = s.platform.ReactDone(ctx, msg.ReplyCtx)
	success = errText == ""
}

func (s *Service) handleCommand(ctx context.Context, msg Message) bool {
	text := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(text, "/") {
		return false
	}
	name, rest, _ := strings.Cut(text, " ")
	name = strings.ToLower(name)
	record := func(success bool) {
		if err := s.recordUsage(ctx, UsageEvent{
			Time:       time.Now(),
			SessionKey: msg.SessionKey,
			MessageID:  msg.MessageID,
			ChatID:     msg.ChatID,
			ChatType:   msg.ChatType,
			UserID:     msg.UserID,
			Kind:       "command",
			Command:    name,
			Success:    success,
			TextChars:  len([]rune(msg.Text)),
		}); err != nil {
			slog.Warn("bridge: record command usage failed", "command", name, "error", err)
		}
	}
	switch name {
	case "/help", "/start":
		_ = s.platform.Send(ctx, msg.ReplyCtx, strings.TrimSpace(`飞书 Codex 远程桥接已连接。

直接发送任务即可让本机 Codex 执行；群聊需要 @机器人。

命令:
/new - 为当前聊天开启新的 Codex 会话
/status - 查看当前会话绑定的 Codex thread_id
/sessions - 列出最近会话
/stats - 查看使用统计
/whoami - 查看你的飞书用户标识
/help - 显示帮助`))
		record(true)
		return true
	case "/new":
		ok := true
		if err := s.store.Reset(msg.SessionKey); err != nil {
			ok = false
			_ = s.platform.Send(ctx, msg.ReplyCtx, "重置会话失败: "+err.Error())
		} else {
			_ = s.platform.Send(ctx, msg.ReplyCtx, "已为当前聊天开启新的 Codex 会话。")
		}
		record(ok)
		return true
	case "/status":
		state := s.store.Get(msg.SessionKey)
		if state.ThreadID == "" {
			_ = s.platform.Send(ctx, msg.ReplyCtx, "当前聊天还没有绑定 Codex 会话。")
		} else {
			_ = s.platform.Send(ctx, msg.ReplyCtx, fmt.Sprintf("当前 Codex thread_id: %s\n更新时间: %s", state.ThreadID, state.UpdatedAt.Format(time.RFC3339)))
		}
		record(true)
		return true
	case "/sessions":
		states := s.store.List()
		if len(states) == 0 {
			_ = s.platform.Send(ctx, msg.ReplyCtx, "暂无已保存会话。")
			record(true)
			return true
		}
		var b strings.Builder
		b.WriteString("最近会话:\n")
		for i, state := range states {
			if i >= 10 {
				break
			}
			b.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n", i+1, state.Key, state.ThreadID, state.UpdatedAt.Format("2006-01-02 15:04:05")))
		}
		_ = s.platform.Send(ctx, msg.ReplyCtx, b.String())
		record(true)
		return true
	case "/stats":
		_ = rest
		_ = s.platform.Send(ctx, msg.ReplyCtx, s.usage.Report(10))
		record(true)
		return true
	case "/whoami":
		_ = rest
		_ = s.platform.Send(ctx, msg.ReplyCtx, s.whoamiText(ctx, msg))
		record(true)
		return true
	case "/reset":
		_ = rest
		ok := true
		if err := s.store.Reset(msg.SessionKey); err != nil {
			ok = false
			_ = s.platform.Send(ctx, msg.ReplyCtx, "重置会话失败: "+err.Error())
		} else {
			_ = s.platform.Send(ctx, msg.ReplyCtx, "当前聊天会话已重置。")
		}
		record(ok)
		return true
	default:
		return false
	}
}

func (s *Service) recordUsage(ctx context.Context, event UsageEvent) error {
	if s.usage == nil {
		return nil
	}
	s.enrichUsageUser(ctx, &event)
	return s.usage.Record(event)
}

func (s *Service) enrichUsageUser(ctx context.Context, event *UsageEvent) {
	if event == nil || strings.TrimSpace(event.UserID) == "" {
		return
	}
	profile, err := s.resolveUser(ctx, event.UserID)
	if err != nil {
		slog.Debug("bridge: resolve usage user failed", "user_id", event.UserID, "error", err)
	}
	if strings.TrimSpace(profile.Name) != "" {
		event.FeishuUserName = profile.Name
	}
	if strings.TrimSpace(profile.EmployeeNo) != "" {
		event.FeishuEmployeeNo = profile.EmployeeNo
	}
}

func (s *Service) resolveUser(ctx context.Context, userID string) (UserProfile, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return UserProfile{}, nil
	}
	resolver, ok := s.platform.(UserResolver)
	if !ok {
		return UserProfile{ID: userID}, nil
	}
	profile, err := resolver.ResolveUser(ctx, userID)
	if strings.TrimSpace(profile.ID) == "" {
		profile.ID = userID
	}
	return profile, err
}

func (s *Service) whoamiText(ctx context.Context, msg Message) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("你的飞书用户标识：%s\n", fallback(msg.UserID, "(unknown)")))
	profile, err := s.resolveUser(ctx, msg.UserID)
	if strings.TrimSpace(profile.Name) != "" {
		b.WriteString("姓名：" + profile.Name + "\n")
	}
	if strings.TrimSpace(profile.EmployeeNo) != "" {
		b.WriteString("工号：" + profile.EmployeeNo + "\n")
	}
	if err != nil && strings.TrimSpace(profile.Name) == "" {
		b.WriteString("姓名：暂未自动获取。请确认飞书后台已添加 contact:user.base:readonly 权限，并发布应用新版本。\n")
	}
	b.WriteString(fmt.Sprintf("当前聊天：%s\n聊天类型：%s\n", fallback(msg.ChatID, "(unknown)"), fallback(msg.ChatType, "(unknown)")))
	b.WriteString("\n统计会优先使用姓名/工号；拿不到时会使用上面的用户标识。")
	return b.String()
}

func fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}
