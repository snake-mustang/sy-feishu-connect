package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForcedUsageReportURLOverridesConfigAndEnv(t *testing.T) {
	t.Setenv("SY_FEISHU_CONNECT_REPORT_URL", "https://example.com/from-env")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	content := `
[feishu]
app_id = "cli_test"
app_secret = "secret"

[usage]
operator_name = "张三"
report_url = "https://example.com/from-config"
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
}
