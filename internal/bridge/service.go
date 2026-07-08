package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

type Options struct {
	Agent         Agent
	Platform      Platform
	DataDir       string
	QueueMessages bool
	Usage         UsageOptions
	Runtime       RuntimeInfo
}

type Service struct {
	agent         Agent
	platform      Platform
	store         *Store
	usage         *UsageTracker
	queueMessages bool
	runtime       RuntimeInfo
	mu            sync.Mutex
	sessions      map[string]*sessionWorker
	activeTurns   map[string]*activeTurn
	latestSession map[string]string
	displayMode   string
}

type activeTurn struct {
	cancel context.CancelFunc
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
		runtime:       opts.Runtime,
		sessions:      map[string]*sessionWorker{},
		activeTurns:   map[string]*activeTurn{},
		latestSession: map[string]string{},
		displayMode:   displayThinking,
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
	msg = s.bindMessageSession(msg)
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
	turnCtx, cancel := context.WithCancel(ctx)
	turn := s.setActiveTurn(msg.SessionKey, cancel)
	defer func() {
		s.clearActiveTurn(msg.SessionKey, turn)
		cancel()
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
	events, err := s.agent.Run(turnCtx, AgentRequest{SessionID: state.ThreadID, Prompt: msg.Text})
	if err != nil {
		errText = err.Error()
		_ = s.platform.Send(ctx, msg.ReplyCtx, "Codex 启动失败: "+err.Error())
		return
	}

	var finalParts []string
	var finalUsage *TokenUsage
	var lastTool time.Time
	for event := range events {
		if event.SessionID != "" {
			if err := s.store.SetThread(msg.SessionKey, event.SessionID); err != nil {
				slog.Warn("bridge: save session failed", "session_key", msg.SessionKey, "error", err)
			}
		}
		switch event.Type {
		case EventThinking:
			text := strings.TrimSpace(event.Text)
			if text != "" && s.getDisplayMode() == displayThinking {
				thinking := formatThinkingText(text)
				replyChars += len([]rune(thinking))
				_ = s.platform.Send(ctx, msg.ReplyCtx, thinking)
			}
		case EventTool:
			text := strings.TrimSpace(event.Text)
			if text != "" {
				if s.getDisplayMode() == displayThinking {
					progress := formatProgressText(text)
					replyChars += len([]rune(progress))
					_ = s.platform.Send(ctx, msg.ReplyCtx, progress)
				}
				if time.Since(lastTool) > 5*time.Second {
					lastTool = time.Now()
					slog.Info("codex progress", "session_key", msg.SessionKey, "event", text)
				}
			}
		case EventText:
			if strings.TrimSpace(event.Text) != "" {
				finalParts = append(finalParts, event.Text)
			}
		case EventError:
			if event.Err != nil {
				if turnCtx.Err() != nil {
					errText = "stopped"
					continue
				}
				errText = event.Err.Error()
				_ = s.platform.Send(ctx, msg.ReplyCtx, "Codex 执行失败: "+event.Err.Error())
			}
		case EventDone:
			if event.Usage != nil {
				finalUsage = event.Usage
			}
		}
	}
	final := strings.TrimSpace(strings.Join(finalParts, "\n\n"))
	if final == "" && turnCtx.Err() != nil {
		errText = "stopped"
		return
	}
	if final == "" && errText != "" {
		return
	}
	if final == "" {
		final = "Codex 已完成，但没有返回文本。"
	}
	final = appendReplyFooter(final, buildReplyFooter(s.runtime, finalUsage))
	replyChars += len([]rune(final))
	if err := s.platform.Send(ctx, msg.ReplyCtx, final); err != nil {
		errText = err.Error()
		slog.Error("bridge: send reply failed", "error", err)
		return
	}
	_ = s.platform.ReactDone(ctx, msg.ReplyCtx)
	success = errText == ""
}

func (s *Service) handleCommand(ctx context.Context, msg Message) bool {
	text := normalizeCommandText(msg.Text)
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
/stop - 停止当前会话正在执行的 Codex
/pwd - 查看 Codex 工作目录
/mode - 查看当前执行模式
/model - 查看当前模型配置
/display thinking|final|quiet - 切换显示模式，默认显示思考过程
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
	case "/stop":
		_ = rest
		if s.stopTurn(msg.SessionKey) {
			_ = s.platform.Send(ctx, msg.ReplyCtx, "已请求停止当前 Codex 执行。")
		} else {
			_ = s.platform.Send(ctx, msg.ReplyCtx, "当前会话没有正在执行的 Codex 任务。")
		}
		record(true)
		return true
	case "/pwd":
		_ = rest
		workDir := fallback(s.runtime.WorkDir, "(未配置)")
		_ = s.platform.Send(ctx, msg.ReplyCtx, "当前 Codex 工作目录:\n"+workDir)
		record(true)
		return true
	case "/mode":
		_ = rest
		_ = s.platform.Send(ctx, msg.ReplyCtx, "当前 Codex 模式: "+fallback(s.runtime.Mode, "suggest")+"\n\nsuggest=只读建议；auto-edit=可编辑工作区；yolo=跳过审批和沙箱。")
		record(true)
		return true
	case "/model":
		_ = rest
		model := strings.TrimSpace(s.runtime.Model)
		if model == "" {
			model = "Codex 默认模型"
		}
		effort := strings.TrimSpace(s.runtime.ReasoningEffort)
		if effort == "" {
			effort = "默认"
		}
		_ = s.platform.Send(ctx, msg.ReplyCtx, fmt.Sprintf("当前模型: %s\n推理强度: %s", model, effort))
		record(true)
		return true
	case "/display":
		mode, ok := normalizeDisplayMode(rest)
		if strings.TrimSpace(rest) == "" {
			_ = s.platform.Send(ctx, msg.ReplyCtx, displayModeText(s.getDisplayMode(), false))
			record(true)
			return true
		}
		if !ok {
			_ = s.platform.Send(ctx, msg.ReplyCtx, "不认识这个显示模式。\n\n可用：/display thinking、/display final、/display quiet")
			record(false)
			return true
		}
		s.setDisplayMode(mode)
		_ = s.platform.Send(ctx, msg.ReplyCtx, displayModeText(mode, true))
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

func normalizeCommandText(raw string) string {
	text := strings.TrimSpace(raw)
	if strings.HasPrefix(text, "/") {
		return text
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "新建会话":
		return "/new"
	case "会话列表":
		return "/sessions"
	case "当前会话", "当前状态":
		return "/status"
	case "停止执行":
		return "/stop"
	case "工作目录":
		return "/pwd"
	case "模式":
		return "/mode"
	case "模型":
		return "/model"
	case "帮助":
		return "/help"
	case "显示思考", "显示思考（默认）", "显示思考(默认)":
		return "/display thinking"
	case "关闭思考":
		return "/display final"
	case "极简模式":
		return "/display quiet"
	default:
		return text
	}
}

func (s *Service) bindMessageSession(msg Message) Message {
	if strings.TrimSpace(msg.UserID) == "" {
		return msg
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg.ChatType == "menu" {
		if latest := s.latestSession[msg.UserID]; latest != "" {
			msg.SessionKey = latest
		} else if strings.TrimSpace(msg.SessionKey) == "" {
			msg.SessionKey = "feishu:menu:" + msg.UserID
		}
		return msg
	}
	if strings.TrimSpace(msg.SessionKey) != "" {
		s.latestSession[msg.UserID] = msg.SessionKey
	}
	return msg
}

func (s *Service) setActiveTurn(sessionKey string, cancel context.CancelFunc) *activeTurn {
	if strings.TrimSpace(sessionKey) == "" || cancel == nil {
		return nil
	}
	turn := &activeTurn{cancel: cancel}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeTurns[sessionKey] = turn
	return turn
}

func (s *Service) clearActiveTurn(sessionKey string, turn *activeTurn) {
	if strings.TrimSpace(sessionKey) == "" || turn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeTurns[sessionKey] == turn {
		delete(s.activeTurns, sessionKey)
	}
}

func (s *Service) stopTurn(sessionKey string) bool {
	s.mu.Lock()
	turn := s.activeTurns[sessionKey]
	s.mu.Unlock()
	if turn == nil || turn.cancel == nil {
		return false
	}
	turn.cancel()
	return true
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
	b.WriteString("\n统计会优先使用姓名；拿不到时会使用上面的用户标识，后续也能人工对应真实姓名。")
	return b.String()
}

func fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

const (
	displayThinking = "thinking"
	displayFinal    = "final"
	displayQuiet    = "quiet"
)

func normalizeDisplayMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "thinking", "think", "on", "full", "show":
		return displayThinking, true
	case "final", "off", "compact", "result", "answer":
		return displayFinal, true
	case "quiet", "minimal", "mini":
		return displayQuiet, true
	default:
		return "", false
	}
}

func (s *Service) getDisplayMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.displayMode == "" {
		return displayThinking
	}
	return s.displayMode
}

func (s *Service) setDisplayMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.displayMode = mode
}

func displayModeText(mode string, changed bool) string {
	prefix := "当前显示模式："
	if changed {
		prefix = "已切换显示模式："
	}
	switch mode {
	case displayThinking:
		return prefix + "显示思考。\n\n会把 Codex 返回的思考摘要、执行过程和工具进度同步到飞书；这是默认模式。"
	case displayQuiet:
		return prefix + "极简模式。\n\n隐藏执行过程，只发送最终回答。"
	default:
		return prefix + "只看结果。\n\n隐藏执行过程，只发送最终回答。"
	}
}

func formatThinkingText(text string) string {
	return "思考中：\n" + strings.TrimSpace(text)
}

func formatProgressText(text string) string {
	return "执行中：\n" + strings.TrimSpace(text)
}

func appendReplyFooter(content, footer string) string {
	footer = strings.TrimSpace(footer)
	if footer == "" {
		return content
	}
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return "---\n" + footer
	}
	return content + "\n\n---\n" + footer
}

func buildReplyFooter(runtime RuntimeInfo, usage *TokenUsage) string {
	hasRuntime := usage != nil ||
		strings.TrimSpace(runtime.WorkDir) != "" ||
		strings.TrimSpace(runtime.Model) != "" ||
		strings.TrimSpace(runtime.ReasoningEffort) != "" ||
		strings.TrimSpace(runtime.CodexHome) != ""
	if !hasRuntime {
		return ""
	}
	var lineParts []string
	model := strings.TrimSpace(runtime.Model)
	effort := strings.TrimSpace(runtime.ReasoningEffort)
	if model == "" || effort == "" {
		defaultModel, defaultEffort := readCodexRuntimeDefaults(runtime.CodexHome)
		if model == "" {
			model = defaultModel
		}
		if effort == "" {
			effort = defaultEffort
		}
	}
	if model == "" && (usage != nil || strings.TrimSpace(runtime.WorkDir) != "" || effort != "") {
		model = "Codex 默认模型"
	}
	if model != "" {
		lineParts = append(lineParts, model)
	}
	if effort != "" {
		lineParts = append(lineParts, "effort:"+effort)
	}
	if usage != nil {
		var counts []string
		if usage.OutputTokens > 0 {
			counts = append(counts, "out "+formatTokenCount(usage.OutputTokens))
		}
		if usage.InputTokens > 0 || usage.CacheCreationInputTokens > 0 || usage.CachedInputTokens > 0 {
			counts = append(counts, fmt.Sprintf("in %s cw %s cr %s",
				formatTokenCount(usage.InputTokens),
				formatTokenCount(usage.CacheCreationInputTokens),
				formatTokenCount(usage.CachedInputTokens)))
		}
		if len(counts) > 0 {
			lineParts = append(lineParts, strings.Join(counts, " "))
		}
		if usage.ContextWindow > 0 {
			used := usage.UsedTokens
			if used <= 0 {
				used = usage.TotalTokens
			}
			if used <= 0 {
				used = usage.InputTokens + usage.OutputTokens
			}
			if used > 0 {
				pct := used * 100 / usage.ContextWindow
				if pct > 100 {
					pct = 100
				}
				lineParts = append(lineParts, fmt.Sprintf("ctx %d%%", pct))
			}
		}
	}
	line := strings.Join(lineParts, " · ")
	workDir := compactPath(strings.TrimSpace(runtime.WorkDir))
	switch {
	case line != "" && workDir != "":
		return line + "\n" + workDir
	case line != "":
		return line
	case workDir != "":
		return workDir
	default:
		return ""
	}
}

func formatTokenCount(n int) string {
	if n < 0 {
		n = 0
	}
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1000000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
}

func compactPath(path string) string {
	if path == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil {
		home = strings.TrimRight(home, "/")
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+"/") {
			return "~" + strings.TrimPrefix(path, home)
		}
	}
	return path
}

func readCodexRuntimeDefaults(codexHome string) (string, string) {
	home := strings.TrimSpace(codexHome)
	if home == "" {
		home = strings.TrimSpace(os.Getenv("CODEX_HOME"))
	}
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", ""
		}
		home = filepath.Join(userHome, ".codex")
	}
	var cfg struct {
		Model                string `toml:"model"`
		ModelReasoningEffort string `toml:"model_reasoning_effort"`
	}
	if _, err := toml.DecodeFile(filepath.Join(home, "config.toml"), &cfg); err != nil {
		return "", ""
	}
	return strings.TrimSpace(cfg.Model), strings.TrimSpace(cfg.ModelReasoningEffort)
}
