package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForcedUsageReportURLOverridesConfigAndEnv(t *testing.T) {
	t.Setenv("SY_FEISHU_CONNECT_REPORT_URL", "https://example.com/from-env")
	t.Setenv("SY_FEISHU_CONNECT_WORKFLOW_REPORT_URL", "https://example.com/from-env-workflow")
	t.Setenv("SY_FEISHU_CONNECT_WORKFLOW_REPORT_TOKEN", "from-env-token")
	t.Setenv("SY_FEISHU_CONNECT_WORKFLOW_PROJECT", "from-env-project")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	content := `
[feishu]
app_id = "cli_test"
app_secret = "secret"

[usage]
operator_name = "张三"
report_url = "https://example.com/from-config"
workflow_report_url = "https://example.com/from-config-workflow"
workflow_report_token = "from-config-token"
workflow_project = "from-config-project"
`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Usage.ReportURL != ForcedUsageReportURL {
		t.Fatalf("report_url=%q", cfg.Usage.ReportURL)
	}
	if cfg.Usage.WorkflowReportURL != "https://lbjqnd425q.feishu.cn/base/workflow/webhook/event/E4C0acdVOwxnCdh7PGYcqaAHnOf" {
		t.Fatalf("workflow_report_url=%q", cfg.Usage.WorkflowReportURL)
	}
	if cfg.Usage.WorkflowToken != "OgMTeAuUiQhPbSJ7b-sAHjUx" {
		t.Fatalf("workflow_report_token=%q", cfg.Usage.WorkflowToken)
	}
	if cfg.Usage.WorkflowProject != "sy-feishu-connect" {
		t.Fatalf("workflow_project=%q", cfg.Usage.WorkflowProject)
	}
}
