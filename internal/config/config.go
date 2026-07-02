package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Feishu FeishuConfig `toml:"feishu"`
	Codex  CodexConfig  `toml:"codex"`
	Bridge BridgeConfig `toml:"bridge"`
	Usage  UsageConfig  `toml:"usage"`
	Log    LogConfig    `toml:"log"`
}

type FeishuConfig struct {
	AppID          string `toml:"app_id"`
	AppSecret      string `toml:"app_secret"`
	Domain         string `toml:"domain"`
	RequireMention bool   `toml:"require_mention"`
	AllowUsers     string `toml:"allow_users"`
	AllowChats     string `toml:"allow_chats"`
	WorkingEmoji   string `toml:"working_emoji"`
	DoneEmoji      string `toml:"done_emoji"`
}

type CodexConfig struct {
	WorkDir         string            `toml:"work_dir"`
	CLIPath         string            `toml:"cli_path"`
	Model           string            `toml:"model"`
	ReasoningEffort string            `toml:"reasoning_effort"`
	Mode            string            `toml:"mode"`
	CodexHome       string            `toml:"codex_home"`
	TurnTimeout     duration          `toml:"turn_timeout"`
	Env             map[string]string `toml:"env"`
}

type BridgeConfig struct {
	DataDir       string `toml:"data_dir"`
	QueueMessages bool   `toml:"queue_messages"`
	MaxReplyChars int    `toml:"max_reply_chars"`
}

type UsageConfig struct {
	OperatorName string `toml:"operator_name"`
	EmployeeID   string `toml:"employee_id"`
	ReportURL    string `toml:"report_url"`
}

type LogConfig struct {
	Level string `toml:"level"`
}

type duration struct {
	time.Duration
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = "config.toml"
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	expanded := os.ExpandEnv(string(content))

	cfg := Default()
	if _, err := toml.Decode(expanded, cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}
	if err := cfg.Normalize(filepath.Dir(path)); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Default() *Config {
	return &Config{
		Feishu: FeishuConfig{
			Domain:         "feishu",
			RequireMention: true,
			AllowUsers:     "*",
			AllowChats:     "*",
			WorkingEmoji:   "OnIt",
			DoneEmoji:      "DONE",
		},
		Codex: CodexConfig{
			WorkDir:     ".",
			CLIPath:     "codex",
			Mode:        "suggest",
			TurnTimeout: duration{Duration: 30 * time.Minute},
		},
		Bridge: BridgeConfig{
			DataDir:       "./data",
			QueueMessages: true,
			MaxReplyChars: 3500,
		},
		Log: LogConfig{Level: "info"},
	}
}

func (c *Config) Normalize(baseDir string) error {
	c.Feishu.AppID = strings.TrimSpace(c.Feishu.AppID)
	c.Feishu.AppSecret = strings.TrimSpace(c.Feishu.AppSecret)
	if c.Feishu.AppID == "" || c.Feishu.AppSecret == "" {
		return fmt.Errorf("feishu.app_id and feishu.app_secret are required")
	}
	if c.Feishu.Domain == "" {
		c.Feishu.Domain = "feishu"
	}
	if c.Feishu.AllowUsers == "" {
		c.Feishu.AllowUsers = "*"
	}
	if c.Feishu.AllowChats == "" {
		c.Feishu.AllowChats = "*"
	}

	if c.Codex.CLIPath == "" {
		c.Codex.CLIPath = "codex"
	}
	if c.Codex.WorkDir == "" {
		c.Codex.WorkDir = "."
	}
	c.Codex.WorkDir = absPath(baseDir, c.Codex.WorkDir)
	if c.Codex.TurnTimeout.Duration == 0 {
		c.Codex.TurnTimeout.Duration = 30 * time.Minute
	}
	c.Codex.Mode = strings.ToLower(strings.TrimSpace(c.Codex.Mode))
	switch c.Codex.Mode {
	case "", "suggest", "auto-edit", "autoedit", "auto_edit", "full-auto", "fullauto", "full_auto", "auto", "yolo":
	default:
		return fmt.Errorf("codex.mode %q is invalid; use suggest, auto-edit, or yolo", c.Codex.Mode)
	}

	if c.Bridge.DataDir == "" {
		c.Bridge.DataDir = "./data"
	}
	c.Bridge.DataDir = absPath(baseDir, c.Bridge.DataDir)
	if c.Bridge.MaxReplyChars <= 0 {
		c.Bridge.MaxReplyChars = 3500
	}
	c.Usage.OperatorName = strings.TrimSpace(c.Usage.OperatorName)
	c.Usage.EmployeeID = strings.TrimSpace(c.Usage.EmployeeID)
	c.Usage.ReportURL = strings.TrimSpace(os.ExpandEnv(c.Usage.ReportURL))
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	return nil
}

func (d *duration) UnmarshalText(text []byte) error {
	raw := strings.TrimSpace(string(text))
	if raw == "" {
		d.Duration = 0
		return nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func (d duration) Std() time.Duration {
	return d.Duration
}

func absPath(baseDir, p string) string {
	p = strings.TrimSpace(os.ExpandEnv(p))
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if baseDir == "" || baseDir == "." {
		if wd, err := os.Getwd(); err == nil {
			baseDir = wd
		}
	}
	return filepath.Clean(filepath.Join(baseDir, p))
}
