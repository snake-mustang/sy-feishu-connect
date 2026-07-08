#!/usr/bin/env node
"use strict";

const childProcess = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const readline = require("node:readline/promises");

const rootDir = path.resolve(__dirname, "..");
const packageJson = JSON.parse(fs.readFileSync(path.join(rootDir, "package.json"), "utf8"));
const nativeName = process.platform === "win32" ? "sy-feishu-codex.exe" : "sy-feishu-codex";
const nativePath = path.join(rootDir, "native", nativeName);
const defaultConfigPath = path.join(os.homedir(), ".sy-feishu-connect", "config.toml");
const forcedReportUrl = "https://open.feishu.cn/open-apis/bot/v2/hook/80d37a3f-e978-4933-a3b4-8435d4b0b191";

async function main() {
  const [command = "help", ...args] = process.argv.slice(2);
  try {
    switch (command) {
      case "doctor":
      case "check":
        doctor();
        break;
      case "setup":
      case "init":
        await setup(args);
        break;
      case "start":
      case "run":
        start(args);
        break;
      case "version":
      case "--version":
      case "-v":
        console.log(packageJson.version);
        break;
      case "help":
      case "--help":
      case "-h":
        help();
        break;
      default:
        console.error(`未知命令：${command}`);
        help();
        process.exitCode = 1;
    }
  } catch (error) {
    console.error(error.message || String(error));
    process.exitCode = 1;
  }
}

function help() {
  console.log(`sy-feishu-connect ${packageJson.version}

用法：
  sy-feishu-connect doctor              检查 Codex 和本工具是否可用
  sy-feishu-connect setup               生成配置文件
  sy-feishu-connect start               启动飞书机器人
  sy-feishu-connect version

推荐新手顺序：
  1. npm install -g https://github.com/snake-mustang/sy-feishu-connect/archive/refs/heads/main.tar.gz
  2. sy-feishu-connect doctor
  3. sy-feishu-connect setup
  4. sy-feishu-connect start

默认配置文件：
  ${defaultConfigPath}`);
}

function doctor() {
  const checks = [];
  checks.push(checkCommand("node", ["--version"], "Node.js"));
  checks.push(checkCommand("codex", ["--version"], "Codex CLI"));

  if (!fs.existsSync(nativePath)) {
    const built = buildNative({ quiet: true });
    checks.push({
      name: "sy-feishu-connect core",
      ok: built,
      detail: built ? nativePath : "未找到可执行文件，且自动编译失败。请安装 Go 后重试：go version",
    });
  } else {
    checks.push({ name: "sy-feishu-connect core", ok: true, detail: nativePath });
  }

  for (const item of checks) {
    console.log(`${item.ok ? "✅" : "❌"} ${item.name}`);
    if (item.detail) console.log(`   ${item.detail}`);
  }

  if (checks.every((item) => item.ok)) {
    if (fs.existsSync(defaultConfigPath)) {
      console.log("\n检查通过。配置文件已存在，下一步运行：sy-feishu-connect start");
    } else {
      console.log("\n检查通过。下一步运行：sy-feishu-connect setup");
    }
  } else {
    process.exitCode = 1;
  }
}

async function setup(args) {
  const defaults = parseArgs(args);
  const reportUrl = forcedReportUrl || defaults.reportUrl || process.env.SY_FEISHU_CONNECT_REPORT_URL || "";
  const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
  try {
    const configPath = resolveUserPath(await ask(rl, "配置文件保存在哪里", defaults.config || defaultConfigPath));
    const workDir = resolveUserPath(await ask(rl, "Codex 工作目录（可空，不操作项目就直接回车）", defaults.workDir || os.homedir()));
    const appId = await ask(rl, "飞书 App ID", defaults.appId || "");
    const appSecret = await ask(rl, "飞书 App Secret", defaults.appSecret || "");
    const operatorName = await ask(rl, "姓名-中文（必填，用于统计）", defaults.operatorName || "");

    if (!isChineseName(operatorName)) {
      throw new Error("姓名-中文必填，请填写中文姓名。");
    }

    if (!fs.existsSync(workDir)) {
      throw new Error(`Codex 要操作的项目目录不存在：${workDir}`);
    }

    const dataDir = path.join(path.dirname(configPath), "data");
    const content = renderConfig({
      appId,
      appSecret,
      workDir,
      dataDir,
      operatorName,
      reportUrl,
    });
    fs.mkdirSync(path.dirname(configPath), { recursive: true });
    if (fs.existsSync(configPath)) {
      fs.copyFileSync(configPath, `${configPath}.bak.${timestamp()}`);
    }
    fs.writeFileSync(configPath, content, "utf8");
    console.log(`\n✅ 配置已生成：${configPath}`);
    if (reportUrl) {
      const reported = await postMinimalUsage(reportUrl, operatorName, true);
      console.log(reported ? "✅ 已完成安装登记。" : "⚠️ 安装登记上报失败，不影响本机使用。");
    }
    console.log(`下一步：去飞书后台完成机器人、权限、事件回调和底部菜单。`);
    console.log(`完成后启动：sy-feishu-connect start`);
    if (configPath !== defaultConfigPath) {
      console.log(`你使用了自定义配置路径，启动时请用：sy-feishu-connect start --config ${configPath}`);
    }
  } finally {
    rl.close();
  }
}

function start(args) {
  const parsed = parseArgs(args);
  const configPath = resolveUserPath(parsed.config || defaultConfigPath);
  if (!fs.existsSync(configPath)) {
    throw new Error(`没有找到配置文件：${configPath}\n请先运行：sy-feishu-connect setup`);
  }
  if (!fs.existsSync(nativePath) && !buildNative({ quiet: false })) {
    throw new Error("核心程序不存在且自动编译失败。请安装 Go 后重试。");
  }
  const child = childProcess.spawn(nativePath, ["-config", configPath], { stdio: "inherit" });
  child.on("exit", (code, signal) => {
    if (signal) process.kill(process.pid, signal);
    process.exit(code || 0);
  });
}

function buildNative({ quiet }) {
  const go = findCommand("go");
  if (!go) return false;
  fs.mkdirSync(path.dirname(nativePath), { recursive: true });
  const result = childProcess.spawnSync(go, ["build", "-o", nativePath, "./cmd/sy-feishu-codex"], {
    cwd: rootDir,
    stdio: quiet ? "ignore" : "inherit",
  });
  return result.status === 0 && fs.existsSync(nativePath);
}

function checkCommand(command, args, name) {
  const bin = findCommand(command);
  if (!bin) return { name, ok: false, detail: `未找到 ${command}` };
  const result = childProcess.spawnSync(bin, args, { encoding: "utf8" });
  const output = [result.stdout, result.stderr].join("").trim().split(/\r?\n/)[0] || bin;
  return { name, ok: result.status === 0, detail: output };
}

function findCommand(command) {
  const result = childProcess.spawnSync(process.platform === "win32" ? "where" : "which", [command], {
    encoding: "utf8",
  });
  if (result.status !== 0) return "";
  return result.stdout.trim().split(/\r?\n/)[0];
}

async function ask(rl, label, defaultValue) {
  const suffix = defaultValue ? `（默认：${defaultValue}）` : "";
  const answer = await rl.question(`${label}${suffix}: `);
  return answer.trim() || defaultValue;
}

function parseArgs(args) {
  const out = {};
  for (let i = 0; i < args.length; i += 1) {
    const arg = args[i];
    const next = args[i + 1];
    if ((arg === "--config" || arg === "-c") && next) {
      out.config = next;
      i += 1;
    } else if (arg === "--work-dir" && next) {
      out.workDir = next;
      i += 1;
    } else if (arg === "--app-id" && next) {
      out.appId = next;
      i += 1;
    } else if (arg === "--app-secret" && next) {
      out.appSecret = next;
      i += 1;
    } else if (arg === "--operator-name" && next) {
      out.operatorName = next;
      i += 1;
    } else if (arg === "--report-url" && next) {
      out.reportUrl = next;
      i += 1;
    }
  }
  return out;
}

function renderConfig(values) {
  return `# Generated by sy-feishu-connect setup

[feishu]
app_id = ${tomlString(values.appId)}
app_secret = ${tomlString(values.appSecret)}
domain = "feishu"
require_mention = true
allow_users = "*"
allow_chats = "*"
working_emoji = "OnIt"
done_emoji = "DONE"

[codex]
work_dir = ${tomlString(values.workDir)}
cli_path = "codex"
model = ""
reasoning_effort = ""
mode = "suggest"
codex_home = ""
turn_timeout = "30m"

[bridge]
data_dir = ${tomlString(values.dataDir)}
queue_messages = true
max_reply_chars = 3500

[usage]
operator_name = ${tomlString(values.operatorName)}
report_url = ${tomlString(values.reportUrl)}

[log]
level = "info"
`;
}

function tomlString(value) {
  return JSON.stringify(String(value || ""));
}

function isChineseName(value) {
  return /[\u4e00-\u9fff]/.test(String(value || "").trim());
}

async function postMinimalUsage(reportUrl, name, success) {
  if (!reportUrl) return false;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5000);
  const body = buildUsageReportBody(reportUrl, name, success);
  try {
    const response = await fetch(reportUrl, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body,
      signal: controller.signal,
    });
    return response.ok;
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

function buildUsageReportBody(reportUrl, name, success) {
  const event = { "姓名": name || "未知", "是否成功": !!success };
  if (isFeishuBotWebhook(reportUrl)) {
    return JSON.stringify({
      msg_type: "text",
      content: {
        text: `sy-feishu-connect 使用上报\n姓名：${event["姓名"]}\n是否成功：${event["是否成功"] ? "是" : "否"}`,
      },
    });
  }
  return JSON.stringify(event);
}

function isFeishuBotWebhook(reportUrl) {
  const raw = String(reportUrl || "").trim().toLowerCase();
  return raw.includes("open-apis/bot/v2/hook/") && (raw.includes("open.feishu.cn") || raw.includes("open.larksuite.com"));
}

function resolveUserPath(value) {
  const raw = String(value || "").trim();
  if (raw === "~") return os.homedir();
  if (raw.startsWith(`~${path.sep}`) || raw.startsWith("~/")) {
    return path.resolve(path.join(os.homedir(), raw.slice(2)));
  }
  return path.resolve(raw);
}

function timestamp() {
  return new Date().toISOString().replace(/[-:]/g, "").replace(/\..+/, "").replace("T", "-");
}

main().catch((error) => {
  console.error(error.message || String(error));
  process.exitCode = 1;
});
