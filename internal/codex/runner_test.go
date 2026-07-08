package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"sy-feishu-codex-webhook/internal/bridge"
)

func TestParserExtractsThreadAndText(t *testing.T) {
	events := make(chan bridge.Event, 8)
	p := newParser(events, context.Background(), "")
	mustHandle(t, p, map[string]any{"type": "thread.started", "thread_id": "t1"})
	mustHandle(t, p, map[string]any{"type": "turn.started"})
	mustHandle(t, p, map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":    "agent_message",
			"content": []any{map[string]any{"type": "output_text", "text": "hello"}},
		},
	})
	mustHandle(t, p, map[string]any{"type": "turn.completed"})
	close(events)

	var got []bridge.Event
	for e := range events {
		got = append(got, e)
	}
	if len(got) != 3 {
		t.Fatalf("events=%#v", got)
	}
	if got[0].Type != bridge.EventStarted || got[0].SessionID != "t1" {
		t.Fatalf("start event=%#v", got[0])
	}
	if got[1].Type != bridge.EventText || got[1].Text != "hello" {
		t.Fatalf("text event=%#v", got[1])
	}
	if got[2].Type != bridge.EventDone || got[2].SessionID != "t1" {
		t.Fatalf("done event=%#v", got[2])
	}
}

func TestParserExtractsTopLevelAgentMessageText(t *testing.T) {
	events := make(chan bridge.Event, 8)
	p := newParser(events, context.Background(), "")
	mustHandle(t, p, map[string]any{"type": "thread.started", "thread_id": "t1"})
	mustHandle(t, p, map[string]any{"type": "turn.started"})
	mustHandle(t, p, map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type": "agent_message",
			"text": "OK",
		},
	})
	mustHandle(t, p, map[string]any{"type": "turn.completed"})
	close(events)

	var texts []string
	for e := range events {
		if e.Type == bridge.EventText {
			texts = append(texts, e.Text)
		}
	}
	if len(texts) != 1 || texts[0] != "OK" {
		t.Fatalf("texts=%#v", texts)
	}
}

func TestParserExtractsReasoningSummary(t *testing.T) {
	events := make(chan bridge.Event, 8)
	p := newParser(events, context.Background(), "")
	mustHandle(t, p, map[string]any{"type": "thread.started", "thread_id": "t1"})
	mustHandle(t, p, map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":    "reasoning",
			"summary": []any{map[string]any{"type": "summary_text", "text": "检查配置并规划下一步"}},
		},
	})
	mustHandle(t, p, map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":    "reasoning",
			"summary": []any{"读取当前问题", "准备回答"},
		},
	})
	close(events)

	var thinking []string
	for e := range events {
		if e.Type == bridge.EventThinking {
			thinking = append(thinking, e.Text)
		}
	}
	want := []string{"检查配置并规划下一步", "读取当前问题\n准备回答"}
	if len(thinking) != len(want) {
		t.Fatalf("thinking=%#v", thinking)
	}
	for i := range want {
		if thinking[i] != want[i] {
			t.Fatalf("thinking[%d]=%q want %q; all=%#v", i, thinking[i], want[i], thinking)
		}
	}
}

func TestParserFlushesPreToolMessageAsThinking(t *testing.T) {
	events := make(chan bridge.Event, 8)
	p := newParser(events, context.Background(), "")
	mustHandle(t, p, map[string]any{"type": "thread.started", "thread_id": "t1"})
	mustHandle(t, p, map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":    "agent_message",
			"content": []any{map[string]any{"type": "output_text", "text": "我先看一下项目结构"}},
		},
	})
	mustHandle(t, p, map[string]any{
		"type": "item.started",
		"item": map[string]any{
			"type":    "command_execution",
			"command": "ls",
		},
	})
	mustHandle(t, p, map[string]any{"type": "turn.completed"})
	close(events)

	var got []bridge.Event
	for e := range events {
		got = append(got, e)
	}
	if len(got) != 4 {
		t.Fatalf("events=%#v", got)
	}
	if got[1].Type != bridge.EventThinking || got[1].Text != "我先看一下项目结构" {
		t.Fatalf("thinking event=%#v", got[1])
	}
	if got[2].Type != bridge.EventTool || got[2].Text != "Bash: ls" {
		t.Fatalf("tool event=%#v", got[2])
	}
}

func TestParserExtractsUsageFromTurnCompleted(t *testing.T) {
	events := make(chan bridge.Event, 8)
	p := newParser(events, context.Background(), "")
	mustHandle(t, p, map[string]any{"type": "thread.started", "thread_id": "t1"})
	mustHandle(t, p, map[string]any{
		"type": "turn.completed",
		"usage": map[string]any{
			"input_tokens":            1200,
			"cached_input_tokens":     700,
			"output_tokens":           80,
			"reasoning_output_tokens": 20,
			"total_tokens":            1280,
			"model_context_window":    4000,
		},
	})
	close(events)

	var done bridge.Event
	for e := range events {
		if e.Type == bridge.EventDone {
			done = e
		}
	}
	if done.Usage == nil {
		t.Fatal("done usage is nil")
	}
	if done.Usage.InputTokens != 1200 || done.Usage.CachedInputTokens != 700 || done.Usage.OutputTokens != 80 || done.Usage.ContextWindow != 4000 {
		t.Fatalf("usage=%#v", done.Usage)
	}
}

func TestParserReadsUsageFromRollout(t *testing.T) {
	codexHome := t.TempDir()
	sessionID := "019f-session"
	sessionDir := filepath.Join(codexHome, "sessions", "2026", "07", "08")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(sessionDir, "rollout-2026-07-08T00-00-00-"+sessionID+".jsonl")
	content := `{"timestamp":"2026-07-08T00:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":5000,"cached_input_tokens":3000,"output_tokens":200,"reasoning_output_tokens":50,"total_tokens":5200},"last_token_usage":{"input_tokens":1500,"cached_input_tokens":900,"output_tokens":60,"reasoning_output_tokens":10,"total_tokens":1560},"model_context_window":10000}}}` + "\n"
	if err := os.WriteFile(rollout, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	events := make(chan bridge.Event, 8)
	p := newParser(events, context.Background(), codexHome)
	mustHandle(t, p, map[string]any{"type": "thread.started", "thread_id": sessionID})
	mustHandle(t, p, map[string]any{"type": "turn.completed"})
	close(events)

	var done bridge.Event
	for e := range events {
		if e.Type == bridge.EventDone {
			done = e
		}
	}
	if done.Usage == nil {
		t.Fatal("done usage is nil")
	}
	if done.Usage.InputTokens != 1500 || done.Usage.CachedInputTokens != 900 || done.Usage.OutputTokens != 60 || done.Usage.ContextWindow != 10000 {
		t.Fatalf("usage=%#v", done.Usage)
	}
}

func TestBuildArgsResumeUsesResumeShape(t *testing.T) {
	r := &Runner{workDir: "/tmp/work", cliBin: "codex", mode: "suggest"}
	args := r.buildArgs("abc")
	want := []string{"exec", "resume", "--skip-git-repo-check", "-c", `sandbox_mode="read-only"`, "-c", `approval_policy="never"`, "abc", "--json", "-"}
	if len(args) != len(want) {
		t.Fatalf("args=%#v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d]=%q want %q; all=%#v", i, args[i], want[i], args)
		}
	}
}

func mustHandle(t *testing.T, p *parser, v map[string]any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.handle(b); err != nil {
		t.Fatal(err)
	}
}
