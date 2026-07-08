package codex

import (
	"context"
	"encoding/json"
	"testing"

	"sy-feishu-codex-webhook/internal/bridge"
)

func TestParserExtractsThreadAndText(t *testing.T) {
	events := make(chan bridge.Event, 8)
	p := newParser(events, context.Background())
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
	p := newParser(events, context.Background())
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
	p := newParser(events, context.Background())
	mustHandle(t, p, map[string]any{"type": "thread.started", "thread_id": "t1"})
	mustHandle(t, p, map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":    "reasoning",
			"summary": []any{map[string]any{"type": "summary_text", "text": "检查配置并规划下一步"}},
		},
	})
	close(events)

	var thinking []string
	for e := range events {
		if e.Type == bridge.EventThinking {
			thinking = append(thinking, e.Text)
		}
	}
	if len(thinking) != 1 || thinking[0] != "检查配置并规划下一步" {
		t.Fatalf("thinking=%#v", thinking)
	}
}

func TestParserFlushesPreToolMessageAsThinking(t *testing.T) {
	events := make(chan bridge.Event, 8)
	p := newParser(events, context.Background())
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
