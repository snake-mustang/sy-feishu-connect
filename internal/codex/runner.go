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
		parser := newParser(events, runCtx)
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
}

func newParser(events chan<- bridge.Event, ctx context.Context) *parser {
	return &parser{events: events, ctx: ctx}
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
		p.emit(bridge.Event{Type: bridge.EventStarted, SessionID: p.sessionID})
	case "turn.started":
		p.pendingText = p.pendingText[:0]
	case "item.started":
		p.handleItemStarted(raw)
	case "item.completed":
		p.handleItemCompleted(raw)
	case "turn.completed":
		p.flushText()
		p.done = true
		p.emit(bridge.Event{Type: bridge.EventDone, SessionID: p.sessionID})
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
			p.emit(bridge.Event{Type: bridge.EventThinking, Text: text, SessionID: p.sessionID})
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
		p.emit(bridge.Event{Type: bridge.EventThinking, Text: text, SessionID: p.sessionID})
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
	if s, ok := item["text"].(string); ok {
		return s
	}
	if s, ok := item[arrayField].(string); ok {
		return s
	}
	arr, ok := item[arrayField].([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, entry := range arr {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if typ := rawString(obj, "type"); typ != "" && typ != elementType {
			continue
		}
		if text := rawString(obj, "text"); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
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
