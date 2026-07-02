#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
sy-feishu-connect 小白配置向导

双击根目录的 .command 后，本脚本会启动一个本地网页向导。
不用 Tk，避免 macOS 上出现空白窗口。
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


REPO_URL = "https://github.com/snake-mustang/sy-feishu-connect.git"
HOME = Path.home()
DEFAULT_INSTALL_DIR = HOME / "sy-feishu-connect"
REPORT_DIR = HOME / ".sy-feishu-connect"
REPORT_FILE = REPORT_DIR / "配置检查与飞书待办报告.html"


def binary_name() -> str:
    return "sy-feishu-codex.exe" if os.name == "nt" else "sy-feishu-codex"


def toml_quote(value: Path | str) -> str:
    return json.dumps(str(value), ensure_ascii=False)


def choose_directory(current: str) -> str:
    initial = Path(current).expanduser() if current else Path.cwd()
    if not initial.exists():
        initial = initial.parent if initial.parent.exists() else Path.cwd()

    if sys.platform == "darwin":
        script = (
            'POSIX path of (choose folder with prompt "选择文件夹" '
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
$dialog.Description = '选择文件夹'
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

    try:
        import tkinter as tk
        from tkinter import filedialog

        root = tk.Tk()
        root.withdraw()
        root.attributes("-topmost", True)
        chosen = filedialog.askdirectory(initialdir=str(initial), title="选择文件夹")
        root.destroy()
        return chosen or ""
    except Exception:
        return ""


@dataclass
class Result:
    name: str
    status: str
    detail: str = ""
    command: str = ""

    @property
    def icon(self) -> str:
        return {"ok": "✅", "fail": "❌", "warn": "⚠️", "info": "ℹ️"}.get(self.status, "ℹ️")


class Runner:
    def __init__(self, form: dict[str, str]) -> None:
        self.install_dir = Path(form.get("install_dir") or str(DEFAULT_INSTALL_DIR)).expanduser()
        self.project_name = form.get("project_name") or "my-project"
        self.work_dir = Path(form.get("work_dir") or os.getcwd()).expanduser()
        self.app_id = (form.get("app_id") or "").strip()
        self.app_secret = (form.get("app_secret") or "").strip()
        self.results: list[Result] = []
        self.logs: list[str] = []

    def run(self) -> str:
        try:
            self._append("开始检查和配置...\n")
            self._check_commands()
            self._prepare_repo()
            self._build_project()
            self._write_config()
            self._add_manual_items()
            self._write_report()
            return self._result_page("完成：配置检查报告已生成", "ok")
        except Exception as exc:
            self.results.append(Result("运行过程异常", "fail", f"{exc}\n\n{traceback.format_exc()}"))
            try:
                self._write_report()
            except Exception:
                pass
            return self._result_page("有失败项：请按报告处理", "fail")

    def _append(self, text: str) -> None:
        self.logs.append(text)

    def _run_cmd(self, cmd: list[str], cwd: Path | None = None, timeout: int = 300) -> tuple[int, str]:
        shown = " ".join(cmd)
        self._append(f"$ {shown}\n")
        proc = subprocess.run(
            cmd,
            cwd=str(cwd) if cwd else None,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=timeout,
        )
        out = proc.stdout or ""
        if out:
            self._append(out[-8000:] + ("\n" if not out.endswith("\n") else ""))
        return proc.returncode, out

    def _check_one(self, name: str, hint: str, version_args: list[str] | None = None) -> None:
        path = shutil.which(name)
        if not path:
            self.results.append(Result(f"检查 {name}", "fail", hint))
            self._append(f"❌ 未找到 {name}：{hint}\n")
            return
        detail = f"路径：{path}"
        if version_args:
            try:
                code, out = self._run_cmd(version_args, timeout=20)
                if code == 0 and out.strip():
                    detail += "\n" + out.strip().splitlines()[0]
            except Exception as exc:
                detail += f"\n版本检查失败：{exc}"
        self.results.append(Result(f"检查 {name}", "ok", detail))
        self._append(f"✅ {name} 可用：{path}\n")

    def _check_commands(self) -> None:
        self._append("\n== 1. 检查本机环境 ==\n")
        self._check_one("git", "请先安装 Git：https://git-scm.com/", ["git", "--version"])
        self._check_one("go", "请先安装 Go 1.25+：https://go.dev/dl/", ["go", "version"])
        self._check_one("codex", "请先安装并登录 Codex CLI，确保终端能运行 codex。", ["codex", "--version"])
        if shutil.which("make"):
            self._check_one("make", "可选：用于执行 make build。", ["make", "--version"])
        else:
            self.results.append(Result("检查 make", "warn", "未找到 make。没关系，配置工具会自动改用 go build 编译，Windows 用户通常不需要额外安装 make。"))
            self._append("⚠️ 未找到 make，将使用 go build 直接编译。\n")
        if any(r.status == "fail" and r.name.startswith("检查 ") for r in self.results):
            raise RuntimeError("环境检查未通过，请先安装缺失工具。")

    def _prepare_repo(self) -> None:
        self._append("\n== 2. 下载或更新 sy-feishu-connect ==\n")
        if (self.install_dir / ".git").exists():
            code, out = self._run_cmd(["git", "pull", "--ff-only"], cwd=self.install_dir, timeout=180)
            self.results.append(Result("更新源码", "ok" if code == 0 else "fail", out.strip(), "git pull --ff-only"))
            if code != 0:
                raise RuntimeError("源码更新失败。")
        elif self.install_dir.exists() and any(self.install_dir.iterdir()):
            self.results.append(Result("下载源码", "fail", f"目录已存在且不是空目录：{self.install_dir}"))
            raise RuntimeError("安装目录已存在且不是空目录。")
        else:
            self.install_dir.parent.mkdir(parents=True, exist_ok=True)
            code, out = self._run_cmd(["git", "clone", REPO_URL, str(self.install_dir)], timeout=300)
            self.results.append(Result("下载源码", "ok" if code == 0 else "fail", out.strip(), f"git clone {REPO_URL}"))
            if code != 0:
                raise RuntimeError("源码下载失败。")

    def _build_project(self) -> None:
        self._append("\n== 3. 编译程序 ==\n")
        bin_dir = self.install_dir / "bin"
        bin_dir.mkdir(parents=True, exist_ok=True)
        binary = bin_dir / binary_name()
        if os.name != "nt" and shutil.which("make"):
            cmd = ["make", "build"]
        else:
            cmd = ["go", "build", "-o", str(binary), "./cmd/sy-feishu-codex"]
        code, out = self._run_cmd(cmd, cwd=self.install_dir, timeout=600)
        if code == 0 and binary.exists():
            self.results.append(Result("编译 sy-feishu-codex", "ok", f"已生成：{binary}", " ".join(cmd)))
        else:
            self.results.append(Result("编译 sy-feishu-codex", "fail", out.strip(), " ".join(cmd)))
            raise RuntimeError("编译失败。")

    def _write_config(self) -> None:
        self._append("\n== 4. 生成配置文件 ==\n")
        config_file = self.install_dir / "config.toml"
        if not self.work_dir.exists():
            self.results.append(Result("检查 work_dir", "fail", f"目录不存在：{self.work_dir}"))
            raise RuntimeError("work_dir 不存在。")
        self.results.append(Result("检查 work_dir", "ok", f"项目目录存在：{self.work_dir}"))

        if config_file.exists():
            backup = config_file.with_suffix(".toml.bak." + _dt.datetime.now().strftime("%Y%m%d-%H%M%S"))
            shutil.copy2(config_file, backup)
            self.results.append(Result("备份旧配置", "warn", f"旧配置已备份到：{backup}"))

        data_dir = self.install_dir / "data"
        content = f'''# Generated by sy-feishu-connect 配置向导

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

[log]
level = "info"
'''
        config_file.write_text(content, encoding="utf-8")
        masked = self.app_secret[:3] + "***" + self.app_secret[-3:] if len(self.app_secret) >= 8 else "***"
        self.results.append(Result("生成配置文件", "ok", f"配置文件：{config_file}\n项目名称：{self.project_name}\nApp ID：{self.app_id or '(未填写)'}\nApp Secret：{masked}"))

    def _add_manual_items(self) -> None:
        self.results.extend([
            Result("飞书后台：创建企业自建应用", "warn", "路径：飞书开放平台 -> 开发者后台 -> 创建企业自建应用。"),
            Result("飞书后台：启用机器人", "warn", "路径：应用能力 -> 机器人。"),
            Result("飞书后台：添加权限并发布", "warn", "至少添加 im:message.p2p_msg:readonly、im:message.group_at_msg:readonly、im:message:send_as_bot。"),
            Result("飞书后台：事件与回调", "warn", "事件 im.message.receive_v1；回调 card.action.trigger；订阅方式都选长连接。"),
            Result("飞书后台：底部自定义栏", "warn", "推荐 4 个菜单：会话、执行、设置、显示。具体见报告。"),
        ])

    def _write_report(self) -> None:
        REPORT_DIR.mkdir(parents=True, exist_ok=True)
        REPORT_FILE.write_text(render_report(self.results, self.logs, self.install_dir), encoding="utf-8")

    def _result_page(self, title: str, status: str) -> str:
        report_url = REPORT_FILE.as_uri() if REPORT_FILE.exists() else "#"
        ok_count = sum(1 for r in self.results if r.status == "ok")
        fail_count = sum(1 for r in self.results if r.status == "fail")
        warn_count = sum(1 for r in self.results if r.status == "warn")
        return page_shell(f"""
<section class="hero compact">
  <div class="eyebrow">sy-feishu-connect</div>
  <h1>{html.escape(title)}</h1>
  <p>{'失败项为 0 时，就可以去飞书后台完成手动配置，然后双击启动机器人。' if status == 'ok' else '请先处理红色失败项；黄色项目是飞书后台必须人工确认的待办。'}</p>
  <div class="stats">
    <div><b>✅ {ok_count}</b><span>通过</span></div>
    <div><b>⚠️ {warn_count}</b><span>待人工确认</span></div>
    <div><b>❌ {fail_count}</b><span>失败</span></div>
  </div>
  <div class="actions">
    <a class="primary" href="{html.escape(report_url)}">打开完整报告</a>
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


def render_report(results: list[Result], logs: list[str], install_dir: Path) -> str:
    generated_at = _dt.datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    start_file = "双击启动机器人.bat" if os.name == "nt" else "双击启动机器人.command"
    start_cmd = ".\\bin\\sy-feishu-codex.exe -config config.toml" if os.name == "nt" else "./bin/sy-feishu-codex -config config.toml"
    return page_shell(f"""
<section class="hero compact">
  <div class="eyebrow">检查报告</div>
  <h1>配置检查与飞书待办报告</h1>
  <p>生成时间：{html.escape(generated_at)}</p>
</section>
{result_table(results)}
<section class="panel">
  <h2>推荐飞书底部自定义栏</h2>
  <div class="menu-grid">
    <div><h3>1. 会话</h3><p>新建会话 <code>/new</code><br>会话列表 <code>/sessions</code><br>当前会话 <code>/status</code></p></div>
    <div><h3>2. 执行</h3><p>停止执行 <code>/stop</code><br>当前状态 <code>/status</code><br>工作目录 <code>/pwd</code></p></div>
    <div><h3>3. 设置</h3><p>模式 <code>/mode</code><br>模型 <code>/model</code><br>帮助 <code>/help</code></p></div>
    <div><h3>4. 显示</h3><p>显示思考 <code>/display full</code><br>关闭思考 <code>/display compact</code><br>极简模式 <code>/display quiet</code></p></div>
  </div>
</section>
<section class="panel">
  <h2>下一步</h2>
  <p>如果失败项是 0，去飞书后台完成黄色待办，然后双击仓库根目录里的 <code>{html.escape(start_file)}</code>。</p>
  <pre>cd {html.escape(str(install_dir))}
{html.escape(start_cmd)}</pre>
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
  <p>这个页面运行在你的电脑本地，只负责检查 Codex、下载或更新源码、编译程序、生成 config.toml 和测试报告。</p>
  <div class="badges"><span>1 双击打开</span><span>2 填 3 个关键信息</span><span>3 看报告</span><span>4 飞书后台手动确认</span><span>5 双击启动机器人</span></div>
</section>
<div class="grid">
  <aside>
    <section class="panel steps">
      <h2>工具自动做</h2>
      <div>检查 Codex / Git / Go / Make</div>
      <div>下载或更新 sy-feishu-connect</div>
      <div>编译 bin/sy-feishu-codex</div>
      <div>生成 config.toml 和检查报告</div>
    </section>
    <section class="panel todo">
      <h2>飞书后台手动做</h2>
      <div>创建企业自建应用</div>
      <div>启用机器人能力</div>
      <div>添加消息权限并发布</div>
      <div>事件/回调选择长连接</div>
      <div>配置底部自定义栏 4 组</div>
    </section>
  </aside>
  <section class="panel">
    <h2>先填这 5 项</h2>
    <p class="note">新手重点确认：Codex 项目目录、飞书 App ID、飞书 App Secret。安装目录默认也能用。</p>
    <form method="post" action="/run">
      <label>安装目录</label>
      <div class="path-row">
        <input id="install_dir" name="install_dir" value="{html.escape(str(DEFAULT_INSTALL_DIR))}">
        <button class="pick" type="button" onclick="chooseDir('install_dir')" title="选择安装目录">...</button>
      </div>
      <p class="hint">工具会在这里下载/更新源码，并生成 bin/sy-feishu-codex。</p>
      <label>项目名称</label>
      <input name="project_name" value="my-project">
      <p class="hint">只是报告里显示用，默认 my-project 即可。</p>
      <label>Codex 项目目录</label>
      <div class="path-row">
        <input id="work_dir" name="work_dir" value="{html.escape(os.getcwd())}">
        <button class="pick" type="button" onclick="chooseDir('work_dir')" title="选择 Codex 项目目录">...</button>
      </div>
      <p class="hint">Codex 会读写这个目录，请填你的真实代码项目路径。</p>
      <label>飞书 App ID</label>
      <input name="app_id" placeholder="cli_xxxxxxxxxxxxx">
      <p class="hint">飞书开放平台 -> 应用后台 -> 凭据与基础信息。</p>
      <label>飞书 App Secret</label>
      <input name="app_secret" type="password" placeholder="只会写入本机 config.toml">
      <div class="actions">
        <button type="submit">一键检查、编译并生成配置</button>
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
            current = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query).get("current", [""])[0]
            chosen = choose_directory(current)
            self._send_json({"path": chosen or ""})
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
