package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"sy-feishu-codex-webhook/internal/bridge"
	"sy-feishu-codex-webhook/internal/codex"
	"sy-feishu-codex-webhook/internal/config"
	"sy-feishu-codex-webhook/internal/feishu"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var configPath string
	var printVersion bool
	flag.StringVar(&configPath, "config", "config.toml", "config file path")
	flag.BoolVar(&printVersion, "version", false, "print version")
	flag.Parse()

	if printVersion {
		fmt.Println(version)
		return nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	setupLogger(cfg.Log.Level)

	runner, err := codex.NewRunner(codex.Options{
		WorkDir:         cfg.Codex.WorkDir,
		CLIPath:         cfg.Codex.CLIPath,
		Model:           cfg.Codex.Model,
		ReasoningEffort: cfg.Codex.ReasoningEffort,
		Mode:            cfg.Codex.Mode,
		CodexHome:       cfg.Codex.CodexHome,
		Env:             cfg.Codex.Env,
		TurnTimeout:     cfg.Codex.TurnTimeout.Std(),
	})
	if err != nil {
		return err
	}

	bot, err := feishu.New(feishu.Options{
		AppID:          cfg.Feishu.AppID,
		AppSecret:      cfg.Feishu.AppSecret,
		Domain:         cfg.Feishu.Domain,
		RequireMention: cfg.Feishu.RequireMention,
		AllowUsers:     cfg.Feishu.AllowUsers,
		AllowChats:     cfg.Feishu.AllowChats,
		WorkingEmoji:   cfg.Feishu.WorkingEmoji,
		DoneEmoji:      cfg.Feishu.DoneEmoji,
		MaxReplyChars:  cfg.Bridge.MaxReplyChars,
	})
	if err != nil {
		return err
	}

	svc, err := bridge.New(bridge.Options{
		Agent:         runner,
		Platform:      bot,
		DataDir:       cfg.Bridge.DataDir,
		QueueMessages: cfg.Bridge.QueueMessages,
		Usage: bridge.UsageOptions{
			OperatorName: cfg.Usage.OperatorName,
			EmployeeID:   cfg.Usage.EmployeeID,
			ReportURL:    cfg.Usage.ReportURL,
		},
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := svc.Start(ctx); err != nil {
		return err
	}
	slog.Info("bridge started", "work_dir", cfg.Codex.WorkDir)
	<-ctx.Done()
	return svc.Close(context.Background())
}

func setupLogger(level string) {
	var slogLevel slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn", "warning":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel})))
}
