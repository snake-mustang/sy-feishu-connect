# Forced Workflow Reporting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Feishu workflow URL, Bearer token, and project name mandatory built-in values while users continue to provide only their Chinese name and optional employee ID.

**Architecture:** The Go configuration normalization layer is the single enforcement point, so old and new configuration files behave identically. CLI and browser setup helpers stop persisting workflow credentials, while existing TOML fields remain parseable for backward compatibility. Runtime usage reporting keeps its current asynchronous HTTP behavior and daily counter.

**Tech Stack:** Go 1.22, Node.js 18+, Python 3, TOML, Feishu workflow webhook

---

### Task 1: Enforce Built-In Workflow Destination

**Files:**
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Write the failing configuration test**

Extend `TestForcedUsageReportURLOverridesConfigAndEnv` so the TOML and environment both contain conflicting workflow values, then assert the built-in constants win:

```go
if cfg.Usage.WorkflowReportURL != ForcedWorkflowReportURL {
	t.Fatalf("workflow_report_url=%q", cfg.Usage.WorkflowReportURL)
}
if cfg.Usage.WorkflowToken != ForcedWorkflowReportToken {
	t.Fatalf("workflow_report_token=%q", cfg.Usage.WorkflowToken)
}
if cfg.Usage.WorkflowProject != ForcedWorkflowProject {
	t.Fatalf("workflow_project=%q", cfg.Usage.WorkflowProject)
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `go test ./internal/config -run TestForcedUsageReportURLOverridesConfigAndEnv -count=1`

Expected: FAIL because workflow values still come from environment/configuration.

- [ ] **Step 3: Add the built-in constants and normalization rule**

Add constants beside `ForcedUsageReportURL`:

```go
const (
	ForcedUsageReportURL     = "https://open.feishu.cn/open-apis/bot/v2/hook/80d37a3f-e978-4933-a3b4-8435d4b0b191"
	ForcedWorkflowReportURL  = "https://lbjqnd425q.feishu.cn/base/workflow/webhook/event/E4C0acdVOwxnCdh7PGYcqaAHnOf"
	ForcedWorkflowReportToken = "OgMTeAuUiQhPbSJ7b-sAHjUx"
	ForcedWorkflowProject    = "sy-feishu-connect"
)
```

After trimming legacy fields in `Normalize`, always assign the three forced workflow values. Keep the fields on `UsageConfig` so old files still decode.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run: `go test ./internal/config -run TestForcedUsageReportURLOverridesConfigAndEnv -count=1`

Expected: PASS.

- [ ] **Step 5: Commit runtime enforcement**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Force workflow usage reporting"
```

### Task 2: Remove Workflow Credentials From Setup Output

**Files:**
- Modify: `cli/sy-feishu-connect.js`
- Modify: `setup-gui.py`
- Modify: `config.example.toml`

- [ ] **Step 1: Capture the pre-change setup surface**

Run:

```bash
rg -n "WORKFLOW_REPORT|workflow_report_(url|token)|workflow_project" cli/sy-feishu-connect.js setup-gui.py config.example.toml
```

Expected: matches show environment variables, hidden form fields, CLI arguments, and generated TOML fields that users no longer need.

- [ ] **Step 2: Remove workflow destination inputs and generated fields**

In the Node setup helper, delete workflow environment constants, `--workflow-*` argument parsing, and these generated lines:

```toml
workflow_report_url = "..."
workflow_report_token = "..."
workflow_project = "..."
```

In the Python setup helper, delete workflow environment constants, `Runner` workflow properties, hidden form inputs, and workflow TOML lines. Keep `operator_name`, `employee_id`, and the existing install-registration `report_url` behavior.

In `config.example.toml`, replace the optional workflow configuration block with a comment explaining that the company build reports structured usage automatically.

- [ ] **Step 3: Verify setup surfaces no longer expose workflow credentials**

Run:

```bash
rg -n "WORKFLOW_REPORT|workflow_report_(url|token)|workflow_project" cli/sy-feishu-connect.js setup-gui.py config.example.toml
```

Expected: no matches.

- [ ] **Step 4: Run syntax checks**

Run:

```bash
node --check cli/sy-feishu-connect.js
python3 -m py_compile setup-gui.py
```

Expected: both commands exit 0.

- [ ] **Step 5: Commit setup cleanup**

```bash
git add cli/sy-feishu-connect.js setup-gui.py config.example.toml
git commit -m "Hide built-in workflow credentials from setup"
```

### Task 3: Update User Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/feishu.md`
- Modify: `使用教程.md`
- Modify: `小白图文教程.html`

- [ ] **Step 1: Replace manual workflow configuration instructions**

Remove `SY_FEISHU_CONNECT_WORKFLOW_REPORT_URL` and `SY_FEISHU_CONNECT_WORKFLOW_REPORT_TOKEN` export instructions. State that the company build automatically reports `用户姓名`, `飞书工号`, `日期`, `当日使用次数`, and `项目`, while failed reporting does not block normal use.

- [ ] **Step 2: Add explicit update instructions**

Document this update flow for existing users:

```bash
npm install -g https://github.com/snake-mustang/sy-feishu-connect/archive/refs/heads/main.tar.gz
sy-feishu-connect doctor
sy-feishu-connect start
```

Explain that existing `~/.sy-feishu-connect/config.toml` is retained, so App ID, App Secret, Chinese name, and employee ID do not need to be entered again. Users must stop the old process and start it again.

- [ ] **Step 3: Check documentation consistency**

Run:

```bash
rg -n "你的飞书流程|你的 Bearer|不要提交到 Git|需要先替换或移除|SY_FEISHU_CONNECT_WORKFLOW" README.md docs/feishu.md 使用教程.md 小白图文教程.html
```

Expected: no stale manual-configuration or non-built-in wording.

- [ ] **Step 4: Commit documentation**

```bash
git add README.md docs/feishu.md 使用教程.md 小白图文教程.html
git commit -m "Document automatic workflow usage reporting"
```

### Task 4: Full Verification And Delivery

**Files:**
- Verify: all changed files

- [ ] **Step 1: Run the complete automated checks**

```bash
go test ./...
node --check cli/sy-feishu-connect.js
python3 -m py_compile setup-gui.py
npm pack --dry-run
git diff --check
```

Expected: every command exits 0.

- [ ] **Step 2: Verify credential enforcement and package contents**

Run focused configuration tests again and confirm the npm package includes the updated runtime sources and setup files:

```bash
go test ./internal/config -run TestForcedUsageReportURLOverridesConfigAndEnv -count=1
npm pack --dry-run
```

Expected: test PASS and package dry-run success.

- [ ] **Step 3: Inspect repository state**

Run: `git status --short --branch`

Expected: only the pre-existing untracked `outputs/` directory remains.

- [ ] **Step 4: Push main**

Run: `git push origin main`

Expected: remote `main` advances to the implementation commit.
