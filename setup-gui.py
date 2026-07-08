#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
sy-feishu-connect browser setup wizard.

This is an optional GUI helper for users who dislike terminal prompts. The
primary installation path is still:

    npm install -g https://github.com/snake-mustang/sy-feishu-connect/archive/refs/heads/main.tar.gz
    sy-feishu-connect doctor
    sy-feishu-connect setup
"""

from __future__ import annotations

import datetime as _dt
import html
import json
import os
import shutil
import socket
import subprocess
import sys
import threading
import traceback
import urllib.parse
import webbrowser
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


HOME = Path.home()
APP_DIR = Path(__file__).resolve().parent
STATE_DIR = HOME / ".sy-feishu-connect"
DEFAULT_CONFIG_FILE = STATE_DIR / "config.toml"
REPORT_FILE = STATE_DIR / "配置检查报告.html"


def toml_quote(value: Path | str) -> str:
    return json.dumps(str(value), ensure_ascii=False)


def choose_directory(current: str) -> str:
    initial = Path(current).expanduser() if current else HOME
    if not initial.exists():
        initial = initial.parent if initial.parent.exists() else HOME

    if sys.platform == "darwin":
        script = (
            'POSIX path of (choose folder with prompt "选择 Codex 要操作的项目目录" '
            f'default location POSIX file {json.dumps(str(initial))})'
        )
        try:
            proc = subprocess.run(["osascript", "-e", script], text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=120)
            return proc.stdout.strip() if proc.returncode == 0 else ""
        except Exception:
            return ""

    if os.name == "nt":
        ps = f"""
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = '选择 Codex 要操作的项目目录'
$dialog.SelectedPath = {json.dumps(str(initial))}
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {{
  [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
  Write-Output $dialog.SelectedPath
}}
"""
        try:
            proc = subprocess.run(
                ["powershell", "-NoProfile", "-STA", "-Command", ps],
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                timeout=120,
            )
            return proc.stdout.strip() if proc.returncode == 0 else ""
        except Exception:
            return ""

    for cmd in (["zenity", "--file-selection", "--directory", "--filename", str(initial)], ["kdialog", "--getexistingdirectory", str(initial)]):
        if shutil.which(cmd[0]):
            try:
                proc = subprocess.run(cmd, text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=120)
                return proc.stdout.strip() if proc.returncode == 0 else ""
            except Exception:
                pass
    return ""


def local_cli_command() -> list[str] | None:
    local_cli = APP_DIR / "cli" / "sy-feishu-connect.js"
    if local_cli.exists() and shutil.which("node"):
        return ["node", str(local_cli)]
    if shutil.which("sy-feishu-connect"):
        return ["sy-feishu-connect"]
    return None


@dataclass
class Result:
    name: str
    status: str
    detail: str = ""

    @property
    def icon(self) -> str:
        return {"ok": "✅", "fail": "❌", "warn": "⚠️", "info": "ℹ️"}.get(self.status, "ℹ️")


class Runner:
    def __init__(self, form: dict[str, str]) -> None:
        self.config_file = Path(form.get("config_file") or str(DEFAULT_CONFIG_FILE)).expanduser()
        self.work_dir = Path(form.get("work_dir") or "").expanduser()
        self.app_id = (form.get("app_id") or "").strip()
        self.app_secret = (form.get("app_secret") or "").strip()
        self.operator_name = (form.get("operator_name") or "").strip()
        self.employee_id = (form.get("employee_id") or "").strip()
        self.report_url = (form.get("report_url") or "").strip()
        self.results: list[Result] = []
        self.logs: list[str] = []

    def run(self) -> str:
        try:
            self._append("开始检查 sy-feishu-connect 配置。\n")
            self._check_environment()
            self._write_config()
            self._add_feishu_todos()
            self._write_report()
            return self._result_page("配置文件已生成", "ok")
        except Exception as exc:
            self.results.append(Result("运行过程异常", "fail", f"{exc}\n\n{traceback.format_exc()}"))
            try:
                self._write_report()
            except Exception:
                pass
            return self._result_page("有失败项，请按报告处理", "fail")

    def _append(self, text: str) -> None:
        self.logs.append(text)

    def _run(self, cmd: list[str], timeout: int = 120) -> tuple[int, str]:
        self._append("$ " + " ".join(cmd) + "\n")
        proc = subprocess.run(cmd, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        out = proc.stdout or ""
        if out:
            self._append(out[-12000:] + ("\n" if not out.endswith("\n") else ""))
        return proc.returncode, out

    def _check_command(self, title: str, command: str, args: list[str], hint: str) -> None:
        path = shutil.which(command)
        if not path:
            self.results.append(Result(title, "fail", hint))
            return
        code, out = self._run([command, *args], timeout=40)
        first = out.strip().splitlines()[0] if out.strip() else path
        self.results.append(Result(title, "ok" if code == 0 else "warn", first))

    def _check_environment(self) -> None:
        self._append("\n== 1. 检查本机环境 ==\n")
        self._check_command("检查 Node.js", "node", ["--version"], "请先安装 Node.js，然后重新打开配置工具。")
        self._check_command("检查 Codex CLI", "codex", ["--version"], "请先安装并登录 Codex CLI。")

        cli = local_cli_command()
        if not cli:
            self.results.append(Result("检查 sy-feishu-connect", "fail", "没有找到 sy-feishu-connect 命令。请先运行：npm install -g https://github.com/snake-mustang/sy-feishu-connect/archive/refs/heads/main.tar.gz"))
            raise RuntimeError("sy-feishu-connect 未安装。")

        code, out = self._run([*cli, "doctor"], timeout=180)
        self.results.append(Result("运行 sy-feishu-connect doctor", "ok" if code == 0 else "fail", out.strip()))
        if code != 0:
            raise RuntimeError("doctor 检查未通过。")

    def _write_config(self) -> None:
        self._append("\n== 2. 生成配置文件 ==\n")
        if not self.work_dir.exists():
            self.results.append(Result("检查 Codex 项目目录", "fail", f"目录不存在：{self.work_dir}"))
            raise RuntimeError("Codex 项目目录不存在。")
        if self.app_id == "" or self.app_secret == "":
            self.results.append(Result("检查飞书凭证", "fail", "App ID 和 App Secret 都不能为空。"))
            raise RuntimeError("飞书凭证未填写。")

        self.config_file.parent.mkdir(parents=True, exist_ok=True)
        if self.config_file.exists():
            backup = self.config_file.with_suffix(".toml.bak." + _dt.datetime.now().strftime("%Y%m%d-%H%M%S"))
            shutil.copy2(self.config_file, backup)
            self.results.append(Result("备份旧配置", "warn", f"旧配置已备份到：{backup}"))

        data_dir = self.config_file.parent / "data"
        content = f'''# Generated by sy-feishu-connect setup wizard

[feishu]
app_id = {toml_quote(self.app_id)}
app_secret = {toml_quote(self.app_secret)}
domain = "feishu"
require_mention = true
allow_users = "*"
allow_chats = "*"
working_emoji = "OnIt"
done_emoji = "DONE"

[codex]
work_dir = {toml_quote(self.work_dir)}
cli_path = "codex"
model = ""
reasoning_effort = ""
mode = "suggest"
codex_home = ""
turn_timeout = "30m"

[bridge]
data_dir = {toml_quote(data_dir)}
queue_messages = true
max_reply_chars = 3500

[usage]
operator_name = {toml_quote(self.operator_name)}
employee_id = {toml_quote(self.employee_id)}
report_url = {toml_quote(self.report_url)}

[log]
level = "info"
'''
        self.config_file.write_text(content, encoding="utf-8")
        self.results.append(Result("生成配置文件", "ok", f"配置文件：{self.config_file}\nCodex 项目目录：{self.work_dir}"))
        if self.report_url:
            self.results.append(Result("统计上报", "ok", f"已配置远程上报地址：{self.report_url}"))
        else:
            self.results.append(Result("统计上报", "warn", "未填写远程上报地址，使用记录只保存在本机。"))

    def _add_feishu_todos(self) -> None:
        self.results.extend([
            Result("飞书后台：创建企业自建应用", "warn", "打开 https://open.feishu.cn/app 创建企业自建应用。"),
            Result("飞书后台：启用机器人", "warn", "路径：应用能力 -> 机器人。"),
            Result("飞书后台：添加权限并发布", "warn", "必选：im:message.p2p_msg:readonly、im:message.group_at_msg:readonly、im:message:send_as_bot。推荐：contact:user.base:readonly，用于统计时自动显示姓名/工号。敏感权限 im:message.group_msg 默认不需要。"),
            Result("飞书后台：事件长连接", "warn", "事件与回调选择长连接，只订阅 im.message.receive_v1。"),
            Result("飞书后台：底部自定义栏", "warn", "推荐 4 组：会话、执行、设置、显示。每个按钮的响应动作都选「发送文字消息」，发送内容填对应的 /命令；不要选「推送事件」。"),
        ])

    def _write_report(self) -> None:
        STATE_DIR.mkdir(parents=True, exist_ok=True)
        REPORT_FILE.write_text(render_report(self.results, self.logs, self.config_file), encoding="utf-8")

    def _result_page(self, title: str, status: str) -> str:
        ok_count = sum(1 for r in self.results if r.status == "ok")
        fail_count = sum(1 for r in self.results if r.status == "fail")
        warn_count = sum(1 for r in self.results if r.status == "warn")
        return page_shell(f"""
<section class="hero compact">
  <div class="eyebrow">sy-feishu-connect</div>
  <h1>{html.escape(title)}</h1>
  <p>{'下一步去飞书后台完成手动配置，然后运行 sy-feishu-connect start。' if status == 'ok' else '请先处理红色失败项；黄色项目是飞书后台必须人工确认的待办。'}</p>
  <div class="stats">
    <div><b>✅ {ok_count}</b><span>通过</span></div>
    <div><b>⚠️ {warn_count}</b><span>待人工确认</span></div>
    <div><b>❌ {fail_count}</b><span>失败</span></div>
  </div>
  <div class="actions">
    <a class="primary" href="{REPORT_FILE.as_uri() if REPORT_FILE.exists() else '#'}">打开完整报告</a>
    <a class="secondary" href="/">返回配置向导</a>
  </div>
</section>
{result_table(self.results)}
<section class="panel"><h2>运行日志</h2><pre class="log">{html.escape(''.join(self.logs)[-30000:])}</pre></section>
""")


def result_table(results: list[Result]) -> str:
    rows = "\n".join(
        f"<tr class='{html.escape(r.status)}'><td>{r.icon}</td><td>{html.escape(r.name)}</td><td><pre>{html.escape(r.detail)}</pre></td></tr>"
        for r in results
    )
    return f"<section class='panel'><h2>检查结果</h2><table><thead><tr><th>状态</th><th>项目</th><th>详情</th></tr></thead><tbody>{rows}</tbody></table></section>"


def render_report(results: list[Result], logs: list[str], config_file: Path) -> str:
    generated_at = _dt.datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    return page_shell(f"""
<section class="hero compact">
  <div class="eyebrow">检查报告</div>
  <h1>配置检查与飞书待办报告</h1>
  <p>生成时间：{html.escape(generated_at)}</p>
</section>
{result_table(results)}
<section class="panel">
  <h2>飞书权限和事件</h2>
  <h3>批量导入权限</h3>
  <p>在飞书后台「权限管理」点击「批量处理」->「批量导入」，直接粘贴：</p>
  <pre>{
  "scopes": {
    "tenant": [
      "contact:user.base:readonly",
      "im:message.group_at_msg:readonly",
      "im:message.p2p_msg:readonly",
      "im:message.group_msg",
      "im:message:send_as_bot"
    ],
    "user": []
  }
}</pre>
  <p><code>im:message.group_msg</code> 是敏感权限。如果你只让群聊 @ 机器人时触发，可以删掉这一行后再导入。</p>
  <h3>必选权限</h3>
  <table>
    <thead><tr><th>权限名称</th><th>权限标识</th><th>用途</th></tr></thead>
    <tbody>
      <tr><td>读取用户发给机器人的单聊消息</td><td><code>im:message.p2p_msg:readonly</code></td><td>接收私聊消息</td></tr>
      <tr><td>获取群组中用户 @ 机器人消息</td><td><code>im:message.group_at_msg:readonly</code></td><td>接收群聊 @ 消息</td></tr>
      <tr><td>以应用身份发送群消息</td><td><code>im:message:send_as_bot</code></td><td>发送回复</td></tr>
    </tbody>
  </table>
  <h3>可选权限</h3>
  <table>
    <thead><tr><th>权限名称</th><th>权限标识</th><th>用途</th></tr></thead>
    <tbody>
      <tr><td>获取与更新用户基本信息</td><td><code>contact:user.base:readonly</code></td><td>自动对应姓名/工号</td></tr>
      <tr><td>获取群组中所有消息</td><td><code>im:message.group_msg</code></td><td>敏感权限；仅关闭 @ 要求时需要</td></tr>
      <tr><td>添加消息表情回复</td><td><code>im:message:reaction</code></td><td>处理中/完成表情</td></tr>
    </tbody>
  </table>
  <h3>事件配置</h3>
  <p>订阅方式选择：使用长连接接收事件。</p>
	  <table>
	    <thead><tr><th>事件名称</th><th>事件标识</th><th>用途</th></tr></thead>
	    <tbody>
	      <tr><td>接收消息</td><td><code>im.message.receive_v1</code></td><td>接收用户发送给机器人的消息</td></tr>
	    </tbody>
	  </table>
	  <p><strong>底部菜单不用订阅 <code>application.bot.menu_v6</code>。</strong>「推送事件」会让飞书服务器向请求地址发 HTTP POST；本机长连接收不到。请把菜单响应动作设为「发送文字消息」。</p>
	</section>
<section class="panel">
  <h2>推荐飞书底部自定义栏</h2>
  <div class="menu-grid">
    <div><h3>1. 会话</h3><p>新建会话 <code>/new</code><br>会话列表 <code>/sessions</code><br>当前会话 <code>/status</code></p></div>
    <div><h3>2. 执行</h3><p>停止执行 <code>/stop</code><br>当前状态 <code>/status</code><br>工作目录 <code>/pwd</code></p></div>
    <div><h3>3. 设置</h3><p>模式 <code>/mode</code><br>模型 <code>/model</code><br>帮助 <code>/help</code></p></div>
    <div><h3>4. 显示</h3><p>显示思考 <code>/display full</code><br>关闭思考 <code>/display compact</code><br>极简模式 <code>/display quiet</code></p></div>
  </div>
	  <p><strong>所有菜单项都选「发送文字消息」。</strong>发送内容直接填上面的 <code>/命令</code>，例如新建会话填 <code>/new</code>。不要选「推送事件」，否则飞书会把点击发到 HTTP 请求地址，本机工具收不到。</p>
	</section>
<section class="panel">
  <h2>下一步</h2>
  <p>如果失败项是 0，去飞书后台完成黄色待办，然后运行：</p>
  <pre>sy-feishu-connect start</pre>
  <p>配置文件：<code>{html.escape(str(config_file))}</code></p>
</section>
<section class="panel"><h2>运行日志</h2><pre class="log">{html.escape(''.join(logs)[-30000:])}</pre></section>
""")


def page_shell(body: str) -> str:
    return f"""<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>sy-feishu-connect 小白配置向导</title>
<style>
*{{box-sizing:border-box}} body{{margin:0;background:#eef3f8;color:#172033;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",Arial,sans-serif;line-height:1.65}}
.wrap{{max-width:1180px;margin:0 auto;padding:28px}}
.hero{{background:#1f5eff;color:white;border-radius:8px;padding:30px;margin-bottom:16px;box-shadow:0 18px 40px rgba(31,94,255,.18)}}
.hero.compact{{padding:26px}} .eyebrow{{font-weight:800;opacity:.85}} h1{{margin:6px 0 10px;font-size:34px;line-height:1.2}} h2{{margin:0 0 14px;font-size:22px}} h3{{margin:0 0 8px}}
.hero p{{max-width:860px;margin:0;color:#eaf1ff}} .badges{{display:flex;gap:8px;flex-wrap:wrap;margin-top:18px}} .badges span{{background:#dbeafe;color:#1d4ed8;border-radius:8px;padding:7px 11px;font-weight:800}}
.grid{{display:grid;grid-template-columns:330px 1fr;gap:16px}} .panel{{background:white;border:1px solid #d8e2ee;border-radius:8px;padding:20px;margin-bottom:16px}}
.steps div,.todo div{{background:#eef4ff;color:#1d4ed8;border-radius:8px;padding:10px 12px;margin:8px 0;font-weight:800}} .todo div{{background:#fff8ed;color:#92400e}}
label{{display:block;font-weight:800;margin:14px 0 6px}} input{{width:100%;height:44px;border:1px solid #cbd5e1;border-radius:8px;padding:0 12px;font-size:15px}} .hint{{margin:5px 0 0;color:#64748b;font-size:13px}}
.path-row{{display:grid;grid-template-columns:1fr 48px;gap:8px}} .pick{{height:44px;padding:0;border-radius:8px;background:#eef4ff;color:#1d4ed8;font-size:18px}}
.actions{{display:flex;gap:10px;flex-wrap:wrap;margin-top:18px}} button,.primary,.secondary{{display:inline-flex;align-items:center;justify-content:center;border:0;border-radius:8px;padding:12px 16px;font-weight:900;text-decoration:none;cursor:pointer}}
button,.primary{{background:#1f5eff;color:white}} .secondary{{background:#eef4ff;color:#1d4ed8}} .note{{background:#f0fdf4;border:1px solid #bbf7d0;border-radius:8px;padding:12px;color:#166534}}
.stats{{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-top:18px;max-width:620px}} .stats div{{background:rgba(255,255,255,.14);border-radius:8px;padding:12px}} .stats b{{display:block;font-size:28px}} .stats span{{color:#eaf1ff}}
table{{width:100%;border-collapse:collapse}} th,td{{border:1px solid #d8e2ee;padding:10px;vertical-align:top;text-align:left}} th{{background:#f3f7fb}} td:first-child{{font-size:22px;text-align:center;width:56px}}
pre{{white-space:pre-wrap;margin:0;font-family:"SFMono-Regular",Consolas,monospace;font-size:13px}} .ok td{{background:#f3fbf5}} .fail td{{background:#fff5f5}} .warn td{{background:#fff8ed}} .log{{background:#0f172a;color:#dbeafe;border-radius:8px;padding:14px;max-height:420px;overflow:auto}}
.menu-grid{{display:grid;grid-template-columns:repeat(2,1fr);gap:12px}} .menu-grid div{{border:1px solid #d8e2ee;border-radius:8px;padding:14px;background:#fbfdff}} code{{background:#eef4ff;color:#1d4ed8;padding:2px 5px;border-radius:5px}}
@media(max-width:860px){{.wrap{{padding:14px}}.grid,.menu-grid,.stats{{grid-template-columns:1fr}}h1{{font-size:28px}}}}
</style>
<script>
async function chooseDir(targetId) {{
  const input = document.getElementById(targetId);
  const current = input ? input.value : "";
  try {{
    const res = await fetch("/choose-dir?current=" + encodeURIComponent(current));
    const data = await res.json();
    if (data && data.path && input) {{
      input.value = data.path;
      input.focus();
    }}
  }} catch (err) {{
    alert("没有打开目录选择窗口，请手动填写路径。");
  }}
}}
</script>
</head>
<body><main class="wrap">{body}</main></body>
</html>"""


def home_page() -> str:
    return page_shell(f"""
<section class="hero">
  <div class="eyebrow">sy-feishu-connect</div>
  <h1>小白配置向导</h1>
  <p>这个页面只做配置和检查。安装请先运行 <code>npm install -g https://github.com/snake-mustang/sy-feishu-connect/archive/refs/heads/main.tar.gz</code>，然后用这里生成配置。</p>
  <div class="badges"><span>1 检查命令</span><span>2 选择项目目录</span><span>3 填飞书密钥</span><span>4 填统计信息</span><span>5 生成报告</span></div>
</section>
<div class="grid">
  <aside>
    <section class="panel steps">
      <h2>工具自动做</h2>
      <div>检查 Node.js / Codex / sy-feishu-connect</div>
      <div>生成 ~/.sy-feishu-connect/config.toml</div>
      <div>生成测试结果报告</div>
      <div>提示下一步启动命令</div>
    </section>
    <section class="panel todo">
      <h2>飞书后台手动做</h2>
      <div>创建企业自建应用</div>
      <div>启用机器人能力</div>
      <div>添加消息权限并发布</div>
      <div>事件选择长连接</div>
      <div>配置底部自定义栏 4 组</div>
    </section>
  </aside>
  <section class="panel">
    <h2>填写配置</h2>
    <p class="note">只填和你有关的内容。不要再纠结安装目录；npm 已经负责安装工具了。</p>
    <form method="post" action="/run">
      <label>配置文件位置</label>
      <input name="config_file" value="{html.escape(str(DEFAULT_CONFIG_FILE))}">
      <p class="hint">不懂就保持默认。</p>

      <label>Codex 要操作的项目目录</label>
      <div class="path-row">
        <input id="work_dir" name="work_dir" value="" placeholder="请选择你的业务项目，比如 /Users/you/code/my-app">
        <button class="pick" type="button" onclick="chooseDir('work_dir')" title="选择 Codex 要操作的项目目录">...</button>
      </div>
      <p class="hint">飞书里的任务会在这个目录里执行。这里填你的真实代码项目路径。</p>

      <label>飞书 App ID</label>
      <input name="app_id" placeholder="cli_xxxxxxxxxxxxx">
      <p class="hint">飞书开放平台 -> 应用后台 -> 凭据与基础信息。</p>

      <label>飞书 App Secret</label>
      <input name="app_secret" type="password" placeholder="只会写入本机 config.toml">

      <label>你的姓名</label>
      <input name="operator_name" placeholder="用于统计是谁安装/使用">

      <label>你的工号</label>
      <input name="employee_id" placeholder="可空">

      <label>统计上报地址</label>
      <input name="report_url" placeholder="可空，例如 n8n webhook 或公司日志接口">
      <p class="hint">不填也能用，统计只保存在本机。普通用户不需要推 GitHub。</p>

      <div class="actions">
        <button type="submit">一键检查并生成配置</button>
        <a class="secondary" href="{REPORT_FILE.as_uri() if REPORT_FILE.exists() else '#'}">打开上次报告</a>
      </div>
    </form>
  </section>
</div>
""")


class Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        if self.path == "/health":
            self._send_json({"ok": True})
            return
        if self.path.startswith("/choose-dir"):
            query = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
            current = query.get("current", [""])[0]
            self._send_json({"path": choose_directory(current) or ""})
            return
        self._send_html(home_page())

    def do_POST(self) -> None:
        if self.path != "/run":
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length).decode("utf-8")
        parsed = urllib.parse.parse_qs(raw)
        form = {key: values[0] if values else "" for key, values in parsed.items()}
        self._send_html(Runner(form).run())

    def log_message(self, fmt: str, *args: object) -> None:
        return

    def _send_html(self, content: str) -> None:
        data = content.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _send_json(self, payload: dict[str, object]) -> None:
        data = json.dumps(payload).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def find_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def main() -> None:
    STATE_DIR.mkdir(parents=True, exist_ok=True)
    port = find_port()
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    url = f"http://127.0.0.1:{port}/"
    print("sy-feishu-connect 小白配置向导已启动")
    print(f"浏览器地址：{url}")
    print("请不要关闭这个窗口；配置完成后可以按 Ctrl+C 退出。")
    threading.Timer(0.4, lambda: webbrowser.open(url)).start()
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
