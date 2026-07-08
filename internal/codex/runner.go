package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"sy-feishu-codex-webhook/internal/bridge"
)

const (
	codexRolloutTailBytes int64 = 1 << 20
	codexUsageRetryDelay        = 50 * time.Millisecond
	codexUsageRetryCount        = 4
)

type Options struct {
	WorkDir         string
	CLIPath         string
	Model           string
	ReasoningEffort string
	Mode            string
	CodexHome       string
	Env             map[string]string
	TurnTimeout     time.Duration
}

type Runner struct {
	workDir         string
	cliBin          string
	cliExtraArgs    []string
	model           string
	reasoningEffort string
	mode            string
	codexHome       string
	env             map[string]string
	turnTimeout     time.Duration
}

func NewRunner(opts Options) (*Runner, error) {
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	abs, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("codex work_dir: %w", err)
	}

	parts := strings.Fields(opts.CLIPath)
	if len(parts) == 0 {
		parts = []string{"codex"}
	}
	if _, err := exec.LookPath(parts[0]); err != nil {
		return nil, fmt.Errorf("codex CLI %q not found in PATH: %w", parts[0], err)
	}
	if opts.TurnTimeout <= 0 {
		opts.TurnTimeout = 30 * time.Minute
	}
	return &Runner{
		workDir:         abs,
		cliBin:          parts[0],
		cliExtraArgs:    parts[1:],
		model:           strings.TrimSpace(opts.Model),
		reasoningEffort: normalizeEffort(opts.ReasoningEffort),
		mode:            normalizeMode(opts.Mode),
		codexHome:       strings.TrimSpace(opts.CodexHome),
		env:             opts.Env,
		turnTimeout:     opts.TurnTimeout,
	}, nil
}

func (r *Runner) Run(ctx context.Context, req bridge.AgentRequest) (<-chan bridge.Event, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("codex: empty prompt")
	}
	runCtx, cancel := context.WithTimeout(ctx, r.turnTimeout)
	events := make(chan bridge.Event, 64)

	args := r.buildArgs(req.SessionID)
	slog.Info("codex: launching", "resume", req.SessionID != "", "work_dir", r.workDir, "args", redactArgs(args))
	cmd := exec.CommandContext(runCtx, r.cliBin, args...)
	cmd.Dir = r.workDir
	cmd.Stdin = strings.NewReader(req.Prompt)
	cmd.Env = r.environ()
	prepareCmdForKill(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("codex: start: %w", err)
	}

	go func() {
		defer cancel()
		defer close(events)
		parser := newParser(events, runCtx, resolveCodexHome(r.codexHome, r.env))
		if err := readJSONLines(stdout, parser.handle); err != nil && runCtx.Err() == nil {
			events <- bridge.Event{Type: bridge.EventError, Err: fmt.Errorf("codex stdout: %w", err)}
		}
		waitErr := cmd.Wait()
		if runCtx.Err() != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			events <- bridge.Event{Type: bridge.EventError, Err: fmt.Errorf("codex turn timed out after %s", r.turnTimeout)}
		} else if waitErr != nil {
			errText := strings.TrimSpace(stderr.String())
			if errText == "" {
				errText = waitErr.Error()
			}
			events <- bridge.Event{Type: bridge.EventError, Err: fmt.Errorf("%s", errText)}
		}
		parser.finish()
	}()

	return events, nil
}

func (r *Runner) buildArgs(sessionID string) []string {
	var args []string
	if sessionID != "" {
		args = []string{"exec", "resume", "--skip-git-repo-check"}
		args = append(args, r.modeArgs(true)...)
	} else {
		args = []string{"exec", "--skip-git-repo-check"}
		args = append(args, r.modeArgs(false)...)
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	if r.reasoningEffort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", r.reasoningEffort))
	}
	if sessionID != "" {
		args = append(args, sessionID, "--json", "-")
	} else {
		args = append(args, "--json", "--cd", r.workDir, "-")
	}
	if len(r.cliExtraArgs) > 0 {
		args = append(append([]string{}, r.cliExtraArgs...), args...)
	}
	return args
}

func (r *Runner) modeArgs(resume bool) []string {
	switch r.mode {
	case "auto-edit", "full-auto":
		if resume {
			return []string{"-c", `sandbox_mode="workspace-write"`, "-c", `approval_policy="never"`}
		}
		return []string{"--sandbox", "workspace-write", "-c", `approval_policy="never"`}
	case "yolo":
		return []string{"--dangerously-bypass-approvals-and-sandbox"}
	default:
		if resume {
			return []string{"-c", `sandbox_mode="read-only"`, "-c", `approval_policy="never"`}
		}
		return []string{"--sandbox", "read-only", "-c", `approval_policy="never"`}
	}
}

func (r *Runner) environ() []string {
	env := os.Environ()
	if r.codexHome != "" {
		env = upsertEnv(env, "CODEX_HOME", r.codexHome)
	}
	for k, v := range r.env {
		env = upsertEnv(env, k, v)
	}
	return env
}

func normalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "auto-edit", "autoedit", "auto_edit", "edit":
		return "auto-edit"
	case "full-auto", "fullauto", "full_auto", "auto":
		return "full-auto"
	case "yolo", "bypass", "dangerously-bypass":
		return "yolo"
	default:
		return "suggest"
	}
}

func normalizeEffort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low", "medium", "high", "xhigh":
		return strings.ToLower(strings.TrimSpace(raw))
	case "med":
		return "medium"
	case "x-high", "very-high":
		return "xhigh"
	default:
		return ""
	}
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

type parser struct {
	events      chan<- bridge.Event
	ctx         context.Context
	mu          sync.Mutex
	pendingText []string
	done        bool
	sessionID   string
	codexHome   string
	sessionFile string
	usage       *bridge.TokenUsage
}

func newParser(events chan<- bridge.Event, ctx context.Context, codexHome string) *parser {
	return &parser{events: events, ctx: ctx, codexHome: codexHome}
}

func (p *parser) handle(line []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		slog.Debug("codex: non-json output", "line", string(line))
		return nil
	}
	switch rawString(raw, "type") {
	case "thread.started":
		p.sessionID = rawString(raw, "thread_id")
		p.sessionFile = ""
		p.usage = nil
		p.emit(bridge.Event{Type: bridge.EventStarted, SessionID: p.sessionID})
	case "turn.started":
		p.pendingText = p.pendingText[:0]
		p.usage = nil
	case "item.started":
		p.handleItemStarted(raw)
	case "item.completed":
		p.handleItemCompleted(raw)
	case "turn.completed":
		p.refreshUsage(raw)
		p.flushText()
		p.done = true
		p.emit(bridge.Event{Type: bridge.EventDone, SessionID: p.sessionID, Usage: p.usage})
	case "turn.failed":
		msg := "turn failed"
		if errObj, ok := raw["error"].(map[string]any); ok {
			if s := rawString(errObj, "message"); s != "" {
				msg = s
			}
		}
		p.emit(bridge.Event{Type: bridge.EventError, Err: fmt.Errorf("%s", msg), SessionID: p.sessionID})
	case "error":
		msg := rawString(raw, "message")
		if msg != "" && !strings.Contains(msg, "Reconnecting") && !strings.Contains(msg, "Falling back") {
			p.emit(bridge.Event{Type: bridge.EventError, Err: fmt.Errorf("%s", msg), SessionID: p.sessionID})
		}
	}
	return nil
}

func (p *parser) handleItemStarted(raw map[string]any) {
	item, ok := raw["item"].(map[string]any)
	if !ok {
		return
	}
	switch rawString(item, "type") {
	case "command_execution":
		cmd := rawString(item, "command")
		if cmd != "" {
			p.flushThinking()
			p.emit(bridge.Event{Type: bridge.EventTool, Text: "Bash: " + truncate(cmd, 300), SessionID: p.sessionID})
		}
	case "function_call":
		name := rawString(item, "name")
		if name != "" {
			p.flushThinking()
			p.emit(bridge.Event{Type: bridge.EventTool, Text: "Tool: " + name, SessionID: p.sessionID})
		}
	}
}

func (p *parser) handleItemCompleted(raw map[string]any) {
	item, ok := raw["item"].(map[string]any)
	if !ok {
		return
	}
	switch rawString(item, "type") {
	case "agent_message", "message":
		if text := extractItemText(item, "content", "output_text"); text != "" {
			p.pendingText = append(p.pendingText, text)
		}
	case "reasoning":
		if text := extractItemText(item, "summary", "summary_text"); text != "" {
			p.emitThinking(text)
		}
	case "command_execution":
		status := rawString(item, "status")
		out := strings.TrimSpace(rawString(item, "aggregated_output"))
		if out != "" {
			out = "Bash result (" + status + "): " + truncate(out, 500)
			p.emit(bridge.Event{Type: bridge.EventTool, Text: out, SessionID: p.sessionID})
		}
	case "function_call":
		name := rawString(item, "name")
		status := rawString(item, "status")
		if name != "" {
			p.emit(bridge.Event{Type: bridge.EventTool, Text: fmt.Sprintf("Tool %s %s", name, status), SessionID: p.sessionID})
		}
	}
}

func (p *parser) flushThinking() {
	for _, text := range p.pendingText {
		p.emitThinking(text)
	}
	p.pendingText = p.pendingText[:0]
}

func (p *parser) flushText() {
	for _, text := range p.pendingText {
		p.emit(bridge.Event{Type: bridge.EventText, Text: text, SessionID: p.sessionID})
	}
	p.pendingText = p.pendingText[:0]
}

func (p *parser) finish() {
	if !p.done && p.ctx.Err() == nil {
		p.flushText()
	}
}

func (p *parser) emitThinking(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	p.emit(bridge.Event{Type: bridge.EventThinking, Text: text, SessionID: p.sessionID})
}

func (p *parser) refreshUsage(raw map[string]any) {
	if usage := tokenUsageFromEvent(raw); usage != nil {
		p.usage = usage
		return
	}
	if strings.TrimSpace(p.sessionID) == "" || strings.TrimSpace(p.codexHome) == "" {
		return
	}
	for attempt := 0; attempt < codexUsageRetryCount; attempt++ {
		path := p.sessionFile
		if path == "" {
			path = findSessionFileInCodexHome(p.codexHome, p.sessionID)
		}
		if path != "" {
			usage, err := readTokenUsageFromRollout(path)
			if err == nil && usage != nil {
				p.sessionFile = path
				p.usage = usage
				return
			}
			if attempt == codexUsageRetryCount-1 && err != nil {
				slog.Debug("codex: usage unavailable", "thread_id", p.sessionID, "error", err)
			}
		}
		select {
		case <-time.After(codexUsageRetryDelay):
		case <-p.ctx.Done():
			return
		}
	}
}

func (p *parser) emit(event bridge.Event) {
	select {
	case p.events <- event:
	case <-p.ctx.Done():
	}
}

func readJSONLines(r io.Reader, handle func([]byte) error) error {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) && len(line) == 0 {
			return nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		line = bytes.TrimRight(line, "\r\n")
		if len(line) > 0 {
			if err := handle(line); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func rawString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func extractItemText(item map[string]any, arrayField, elementType string) string {
	if s, ok := item[arrayField].(string); ok {
		return s
	}
	if arr, ok := item[arrayField].([]any); ok {
		var parts []string
		for _, entry := range arr {
			switch v := entry.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					parts = append(parts, v)
				}
			case map[string]any:
				if typ := rawString(v, "type"); typ != "" && typ != elementType {
					continue
				}
				if text := rawString(v, "text"); text != "" {
					parts = append(parts, text)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	if s, ok := item["text"].(string); ok {
		return s
	}
	if arr, ok := item["content"].([]any); ok && arrayField != "content" {
		var parts []string
		for _, entry := range arr {
			text, ok := entry.(string)
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			parts = append(parts, text)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

type tokenUsageSnake struct {
	TotalTokens           int `json:"total_tokens"`
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

type tokenUsageCamel struct {
	TotalTokens           int `json:"totalTokens"`
	InputTokens           int `json:"inputTokens"`
	CachedInputTokens     int `json:"cachedInputTokens"`
	OutputTokens          int `json:"outputTokens"`
	ReasoningOutputTokens int `json:"reasoningOutputTokens"`
}

func tokenUsageFromEvent(raw map[string]any) *bridge.TokenUsage {
	if raw == nil {
		return nil
	}
	if usage := usageFromMap(raw); usage != nil {
		return usage
	}
	for _, key := range []string{"info", "payload", "usage", "token_usage", "tokenUsage"} {
		if child, ok := raw[key].(map[string]any); ok {
			if usage := usageFromMap(child); usage != nil {
				return usage
			}
		}
	}
	return nil
}

func usageFromMap(m map[string]any) *bridge.TokenUsage {
	if m == nil {
		return nil
	}
	contextWindow := firstInt(m, "model_context_window", "modelContextWindow", "context_window", "contextWindow")
	for _, key := range []string{"last_token_usage", "lastTokenUsage", "usage", "token_usage", "tokenUsage"} {
		if child, ok := m[key].(map[string]any); ok {
			if usage := usageFromFlatMap(child, contextWindow); usage != nil {
				return usage
			}
		}
	}
	return usageFromFlatMap(m, contextWindow)
}

func usageFromFlatMap(m map[string]any, contextWindow int) *bridge.TokenUsage {
	inputTokens := firstInt(m, "input_tokens", "inputTokens")
	cachedInputTokens := firstInt(m, "cached_input_tokens", "cachedInputTokens")
	outputTokens := firstInt(m, "output_tokens", "outputTokens")
	reasoningTokens := firstInt(m, "reasoning_output_tokens", "reasoningOutputTokens")
	totalTokens := firstInt(m, "total_tokens", "totalTokens")
	if contextWindow <= 0 {
		contextWindow = firstInt(m, "model_context_window", "modelContextWindow", "context_window", "contextWindow")
	}
	if totalTokens <= 0 && inputTokens <= 0 && outputTokens <= 0 {
		return nil
	}
	usedTokens := currentContextTokens(totalTokens, inputTokens, outputTokens)
	return &bridge.TokenUsage{
		UsedTokens:            usedTokens,
		TotalTokens:           totalTokens,
		InputTokens:           inputTokens,
		CachedInputTokens:     cachedInputTokens,
		OutputTokens:          outputTokens,
		ReasoningOutputTokens: reasoningTokens,
		ContextWindow:         contextWindow,
	}
}

func firstInt(m map[string]any, keys ...string) int {
	for _, key := range keys {
		switch v := m[key].(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case json.Number:
			n, _ := v.Int64()
			return int(n)
		}
	}
	return 0
}

func resolveCodexHome(configured string, env map[string]string) string {
	if value := strings.TrimSpace(configured); value != "" {
		return value
	}
	if env != nil {
		if value := strings.TrimSpace(env["CODEX_HOME"]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(os.Getenv("CODEX_HOME")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func findSessionFileInCodexHome(codexHome, sessionID string) string {
	codexHome = strings.TrimSpace(codexHome)
	sessionID = strings.TrimSpace(sessionID)
	if codexHome == "" || sessionID == "" {
		return ""
	}
	patterns := []string{
		filepath.Join(codexHome, "sessions", "*", "*", "*", "rollout-*"+sessionID+".jsonl"),
		filepath.Join(codexHome, "archived_sessions", "rollout-*"+sessionID+".jsonl"),
	}
	for _, pattern := range patterns {
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			return matches[len(matches)-1]
		}
	}
	return ""
}

func readTokenUsageFromRollout(path string) (*bridge.TokenUsage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 {
		return nil, nil
	}
	start := int64(0)
	if info.Size() > codexRolloutTailBytes {
		start = info.Size() - codexRolloutTailBytes
	}
	buf := make([]byte, int(info.Size()-start))
	n, err := f.ReadAt(buf, start)
	if err != nil && err != io.EOF {
		return nil, err
	}
	buf = buf[:n]
	if start > 0 {
		if idx := bytes.IndexByte(buf, '\n'); idx >= 0 {
			buf = buf[idx+1:]
		}
	}
	if usage := parseTokenUsageFromRolloutBytes(buf); usage != nil {
		return usage, nil
	}
	return nil, nil
}

func parseTokenUsageFromRolloutBytes(data []byte) *bridge.TokenUsage {
	lines := bytes.Split(data, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		if usage := parseTokenUsageFromRolloutLine(line); usage != nil {
			return usage
		}
	}
	return nil
}

func parseTokenUsageFromRolloutLine(line []byte) *bridge.TokenUsage {
	var entry struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &entry); err != nil || entry.Type != "event_msg" {
		return nil
	}
	var payload struct {
		Type string `json:"type"`
		Info *struct {
			TotalTokenUsage    tokenUsageSnake `json:"total_token_usage"`
			LastTokenUsage     tokenUsageSnake `json:"last_token_usage"`
			ModelContextWindow int             `json:"model_context_window"`
		} `json:"info"`
	}
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		return nil
	}
	if payload.Type != "token_count" || payload.Info == nil {
		return nil
	}
	return tokenUsageFromSnake(payload.Info.LastTokenUsage, payload.Info.ModelContextWindow)
}

func tokenUsageFromSnake(usage tokenUsageSnake, contextWindow int) *bridge.TokenUsage {
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
		return nil
	}
	return &bridge.TokenUsage{
		UsedTokens:            currentContextTokens(usage.TotalTokens, usage.InputTokens, usage.OutputTokens),
		TotalTokens:           usage.TotalTokens,
		InputTokens:           usage.InputTokens,
		CachedInputTokens:     usage.CachedInputTokens,
		OutputTokens:          usage.OutputTokens,
		ReasoningOutputTokens: usage.ReasoningOutputTokens,
		ContextWindow:         contextWindow,
	}
}

func currentContextTokens(totalTokens, inputTokens, outputTokens int) int {
	if totalTokens > 0 {
		return totalTokens
	}
	if inputTokens > 0 || outputTokens > 0 {
		return inputTokens + outputTokens
	}
	return 0
}

func truncate(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxRunes]) + "..."
}

func redactArgs(args []string) []string {
	out := append([]string(nil), args...)
	for i := 0; i < len(out)-1; i++ {
		if out[i] == "-c" && (strings.Contains(out[i+1], "api_key") || strings.Contains(out[i+1], "token")) {
			out[i+1] = "***"
		}
	}
	return out
}
