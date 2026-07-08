package bridge_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"sy-feishu-codex-webhook/internal/bridge"
	"sy-feishu-codex-webhook/internal/codex"
)

func TestBridgeWithFakeCodexCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake CLI is unix-only")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "codex")
	script := `#!/bin/sh
echo '{"type":"thread.started","thread_id":"fake-thread"}'
echo '{"type":"turn.started"}'
echo '{"type":"item.completed","item":{"type":"agent_message","content":[{"type":"output_text","text":"fake reply"}]}}'
echo '{"type":"turn.completed"}'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	runner, err := codex.NewRunner(codex.Options{
		WorkDir:     dir,
		CLIPath:     fake,
		CodexHome:   filepath.Join(dir, "codex-home"),
		TurnTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{}
	svc, err := bridge.New(bridge.Options{
		Agent:         runner,
		Platform:      platform,
		DataDir:       filepath.Join(dir, "data"),
		QueueMessages: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	platform.handler(context.Background(), bridge.Message{SessionKey: "k", Text: "hello"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		platform.mu.Lock()
		done := len(platform.sent) > 0
		platform.mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	platform.mu.Lock()
	defer platform.mu.Unlock()
	if len(platform.sent) != 1 || !strings.Contains(platform.sent[0], "fake reply") {
		t.Fatalf("sent=%#v", platform.sent)
	}
}

type fakePlatform struct {
	mu      sync.Mutex
	handler func(context.Context, bridge.Message)
	sent    []string
}

func (p *fakePlatform) Start(ctx context.Context, h func(context.Context, bridge.Message)) error {
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
