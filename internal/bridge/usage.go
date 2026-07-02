package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type UsageTracker struct {
	mu          sync.Mutex
	eventsPath  string
	summaryPath string
	summary     UsageSummary
}

type UsageEvent struct {
	Time       time.Time `json:"time"`
	SessionKey string    `json:"session_key"`
	MessageID  string    `json:"message_id,omitempty"`
	ChatID     string    `json:"chat_id,omitempty"`
	ChatType   string    `json:"chat_type,omitempty"`
	UserID     string    `json:"user_id,omitempty"`
	Kind       string    `json:"kind"`
	Command    string    `json:"command,omitempty"`
	Success    bool      `json:"success"`
	DurationMS int64     `json:"duration_ms"`
	TextChars  int       `json:"text_chars"`
	ReplyChars int       `json:"reply_chars,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type UsageSummary struct {
	UpdatedAt time.Time                `json:"updated_at"`
	Total     UsageCounter             `json:"total"`
	Users     map[string]*UsageCounter `json:"users"`
	Chats     map[string]*UsageCounter `json:"chats"`
	Commands  map[string]int           `json:"commands"`
}

type UsageCounter struct {
	ID         string    `json:"id,omitempty"`
	Total      int       `json:"total"`
	Tasks      int       `json:"tasks"`
	Commands   int       `json:"commands"`
	Success    int       `json:"success"`
	Failed     int       `json:"failed"`
	TextChars  int       `json:"text_chars"`
	ReplyChars int       `json:"reply_chars"`
	DurationMS int64     `json:"duration_ms"`
	LastSeen   time.Time `json:"last_seen"`
}

func OpenUsageTracker(dataDir string) (*UsageTracker, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	t := &UsageTracker{
		eventsPath:  filepath.Join(dataDir, "usage_events.jsonl"),
		summaryPath: filepath.Join(dataDir, "usage_summary.json"),
		summary: UsageSummary{
			Users:    map[string]*UsageCounter{},
			Chats:    map[string]*UsageCounter{},
			Commands: map[string]int{},
		},
	}
	if b, err := os.ReadFile(t.summaryPath); err == nil {
		if err := json.Unmarshal(b, &t.summary); err != nil {
			return nil, fmt.Errorf("decode %s: %w", t.summaryPath, err)
		}
	}
	t.ensureMaps()
	return t, nil
}

func (t *UsageTracker) Record(event UsageEvent) error {
	if t == nil {
		return nil
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	event.Kind = strings.TrimSpace(event.Kind)
	if event.Kind == "" {
		event.Kind = "task"
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(t.eventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	t.ensureMaps()
	t.summary.UpdatedAt = event.Time
	applyUsage(&t.summary.Total, event, "")
	if event.UserID != "" {
		applyUsage(counterFor(t.summary.Users, event.UserID), event, event.UserID)
	}
	if event.ChatID != "" {
		applyUsage(counterFor(t.summary.Chats, event.ChatID), event, event.ChatID)
	}
	if event.Command != "" {
		t.summary.Commands[event.Command]++
	}
	return t.saveSummaryLocked()
}

func (t *UsageTracker) Report(limit int) string {
	if t == nil {
		return "使用统计未启用。"
	}
	if limit <= 0 {
		limit = 10
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureMaps()

	var b strings.Builder
	total := t.summary.Total
	b.WriteString("使用统计\n")
	b.WriteString(fmt.Sprintf("总消息：%d，普通任务：%d，命令：%d\n", total.Total, total.Tasks, total.Commands))
	b.WriteString(fmt.Sprintf("成功：%d，失败：%d\n", total.Success, total.Failed))
	if !t.summary.UpdatedAt.IsZero() {
		b.WriteString("最后更新：" + t.summary.UpdatedAt.Format("2006-01-02 15:04:05") + "\n")
	}
	b.WriteString("\n按用户 Top:\n")
	users := topCounters(t.summary.Users, limit)
	if len(users) == 0 {
		b.WriteString("暂无用户记录。\n")
	} else {
		for i, u := range users {
			b.WriteString(fmt.Sprintf("%d. %s\n   总计 %d / 任务 %d / 命令 %d / 成功 %d / 失败 %d / 最后 %s\n",
				i+1, u.ID, u.Total, u.Tasks, u.Commands, u.Success, u.Failed, u.LastSeen.Format("2006-01-02 15:04:05")))
		}
	}
	b.WriteString("\n本地文件:\n")
	b.WriteString("原始明细: " + t.eventsPath + "\n")
	b.WriteString("汇总结果: " + t.summaryPath + "\n")
	b.WriteString("\n提示：让用户发送 /whoami，可以把 open_id 对应到真实姓名。")
	return b.String()
}

func (t *UsageTracker) ensureMaps() {
	if t.summary.Users == nil {
		t.summary.Users = map[string]*UsageCounter{}
	}
	if t.summary.Chats == nil {
		t.summary.Chats = map[string]*UsageCounter{}
	}
	if t.summary.Commands == nil {
		t.summary.Commands = map[string]int{}
	}
}

func (t *UsageTracker) saveSummaryLocked() error {
	tmp := t.summaryPath + ".tmp"
	b, err := json.MarshalIndent(t.summary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, t.summaryPath)
}

func counterFor(m map[string]*UsageCounter, id string) *UsageCounter {
	if c := m[id]; c != nil {
		return c
	}
	c := &UsageCounter{ID: id}
	m[id] = c
	return c
}

func applyUsage(c *UsageCounter, event UsageEvent, id string) {
	if id != "" {
		c.ID = id
	}
	c.Total++
	if event.Kind == "command" {
		c.Commands++
	} else {
		c.Tasks++
	}
	if event.Success {
		c.Success++
	} else {
		c.Failed++
	}
	c.TextChars += event.TextChars
	c.ReplyChars += event.ReplyChars
	c.DurationMS += event.DurationMS
	c.LastSeen = event.Time
}

func topCounters(m map[string]*UsageCounter, limit int) []*UsageCounter {
	out := make([]*UsageCounter, 0, len(m))
	for _, c := range m {
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total == out[j].Total {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].Total > out[j].Total
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
