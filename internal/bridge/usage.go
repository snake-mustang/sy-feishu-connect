package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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
	opts        UsageOptions
	client      *http.Client
}

type UsageOptions struct {
	OperatorName string
	EmployeeID   string
	ReportURL    string
}

type UsageEvent struct {
	Time             time.Time `json:"time"`
	OperatorName     string    `json:"operator_name,omitempty"`
	EmployeeID       string    `json:"employee_id,omitempty"`
	SessionKey       string    `json:"session_key"`
	MessageID        string    `json:"message_id,omitempty"`
	ChatID           string    `json:"chat_id,omitempty"`
	ChatType         string    `json:"chat_type,omitempty"`
	UserID           string    `json:"user_id,omitempty"`
	FeishuUserName   string    `json:"feishu_user_name,omitempty"`
	FeishuEmployeeNo string    `json:"feishu_employee_no,omitempty"`
	Kind             string    `json:"kind"`
	Command          string    `json:"command,omitempty"`
	Success          bool      `json:"success"`
	DurationMS       int64     `json:"duration_ms"`
	TextChars        int       `json:"text_chars"`
	ReplyChars       int       `json:"reply_chars,omitempty"`
	Error            string    `json:"error,omitempty"`
}

type RemoteUsageEvent struct {
	Name    string `json:"姓名"`
	Success bool   `json:"是否成功"`
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
	Name       string    `json:"name,omitempty"`
	EmployeeNo string    `json:"employee_no,omitempty"`
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

func OpenUsageTracker(dataDir string, opts UsageOptions) (*UsageTracker, error) {
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
		opts:   sanitizeUsageOptions(opts),
		client: &http.Client{Timeout: 5 * time.Second},
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
	if event.OperatorName == "" {
		event.OperatorName = t.opts.OperatorName
	}
	if event.EmployeeID == "" {
		event.EmployeeID = t.opts.EmployeeID
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
	if err := t.saveSummaryLocked(); err != nil {
		return err
	}
	t.reportAsync(event)
	return nil
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
				i+1, usageCounterLabel(u), u.Total, u.Tasks, u.Commands, u.Success, u.Failed, u.LastSeen.Format("2006-01-02 15:04:05")))
		}
	}
	b.WriteString("\n本地文件:\n")
	b.WriteString("原始明细: " + t.eventsPath + "\n")
	b.WriteString("汇总结果: " + t.summaryPath + "\n")
	if t.opts.ReportURL != "" {
		b.WriteString("远程上报: 已启用（仅上报姓名和是否成功）\n")
	} else {
		b.WriteString("远程上报: 未配置\n")
	}
	b.WriteString("\n提示：如果已开通 contact:user.base:readonly 权限，统计会尽量显示飞书姓名；否则会保留 open_id，后续也能人工对应真实姓名。")
	return b.String()
}

func (t *UsageTracker) reportAsync(event UsageEvent) {
	if t.opts.ReportURL == "" {
		return
	}
	payload, err := json.Marshal(RemoteUsageEvent{
		Name:    remoteUsageName(t.opts, event),
		Success: event.Success,
	})
	if err != nil {
		return
	}
	go func() {
		req, err := http.NewRequest(http.MethodPost, t.opts.ReportURL, bytes.NewReader(payload))
		if err != nil {
			slog.Warn("usage: build report request failed", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := t.client.Do(req)
		if err != nil {
			slog.Warn("usage: report failed", "error", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			slog.Warn("usage: report rejected", "status", resp.Status)
		}
	}()
}

func remoteUsageName(opts UsageOptions, event UsageEvent) string {
	for _, value := range []string{
		opts.OperatorName,
		event.OperatorName,
		event.FeishuUserName,
		event.UserID,
	} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func sanitizeUsageOptions(opts UsageOptions) UsageOptions {
	opts.OperatorName = strings.TrimSpace(opts.OperatorName)
	opts.EmployeeID = strings.TrimSpace(opts.EmployeeID)
	opts.ReportURL = strings.TrimSpace(opts.ReportURL)
	return opts
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
		if strings.TrimSpace(event.FeishuUserName) != "" {
			c.Name = strings.TrimSpace(event.FeishuUserName)
		}
		if strings.TrimSpace(event.FeishuEmployeeNo) != "" {
			c.EmployeeNo = strings.TrimSpace(event.FeishuEmployeeNo)
		}
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

func usageCounterLabel(c *UsageCounter) string {
	if c == nil {
		return ""
	}
	id := strings.TrimSpace(c.ID)
	name := strings.TrimSpace(c.Name)
	employeeNo := strings.TrimSpace(c.EmployeeNo)
	if name == "" {
		return id
	}
	if employeeNo != "" {
		return fmt.Sprintf("%s/%s (%s)", name, employeeNo, id)
	}
	if id != "" {
		return fmt.Sprintf("%s (%s)", name, id)
	}
	return name
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
