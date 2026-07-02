#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
sy-feishu-connect 配置向导

功能：
1. 检查 codex / git / go / make
2. 下载或更新 sy-feishu-connect
3. 编译 make build
4. 引导填写项目路径、飞书 App ID、App Secret
5. 生成仓库根目录 config.toml
6. 生成并自动打开 HTML 测试报告

只依赖 Python 标准库。
"""

from __future__ import annotations

import datetime as _dt
import html
import os
import shutil
import subprocess
import sys
import threading
import traceback
import webbrowser
from dataclasses import dataclass
from pathlib import Path
from tkinter import BOTH, END, LEFT, RIGHT, X, Y, Button, Entry, Frame, Label, StringVar, Text, Tk, filedialog, messagebox


REPO_URL = "https://github.com/snake-mustang/sy-feishu-connect.git"
HOME = Path.home()
DEFAULT_INSTALL_DIR = HOME / "sy-feishu-connect"
REPORT_DIR = HOME / ".sy-feishu-connect"
REPORT_FILE = REPORT_DIR / "配置检查与飞书待办报告.html"


@dataclass
class Result:
    name: str
    status: str  # ok / fail / warn / info
    detail: str = ""
    command: str = ""

    @property
    def icon(self) -> str:
        return {
            "ok": "✅",
            "fail": "❌",
            "warn": "⚠️",
            "info": "ℹ️",
        }.get(self.status, "ℹ️")


class SetupApp:
    def __init__(self) -> None:
        self.root = Tk()
        self.root.title("sy-feishu-connect 配置向导")
        self.root.geometry("920x720")
        self.root.minsize(820, 640)

        self.install_dir = StringVar(value=str(DEFAULT_INSTALL_DIR))
        self.project_name = StringVar(value="my-project")
        self.work_dir = StringVar(value=os.getcwd())
        self.app_id = StringVar(value="")
        self.app_secret = StringVar(value="")

        self.results: list[Result] = []
        self.logs: list[str] = []

        self._build_ui()

    def _build_ui(self) -> None:
        root = self.root

        header = Frame(root, padx=18, pady=16)
        header.pack(fill=X)
        Label(header, text="sy-feishu-connect 配置向导", font=("Arial", 22, "bold")).pack(anchor="w")
        Label(
            header,
            text="这个工具会帮你检查环境、下载源码、编译程序、生成配置，并在结束后自动打开测试报告。",
            fg="#4b5563",
            font=("Arial", 13),
        ).pack(anchor="w", pady=(6, 0))

        form = Frame(root, padx=18)
        form.pack(fill=X)

        self._row(form, "安装目录", self.install_dir, self._choose_install_dir)
        self._row(form, "项目名称", self.project_name, None)
        self._row(form, "Codex 项目目录 work_dir", self.work_dir, self._choose_work_dir)
        self._row(form, "飞书 App ID", self.app_id, None)
        self._row(form, "飞书 App Secret", self.app_secret, None, secret=True)

        tips = Frame(root, padx=18, pady=8)
        tips.pack(fill=X)
        Label(
            tips,
            text="提示：App ID / App Secret 在飞书开放平台 -> 应用后台 -> 凭据与基础信息。飞书权限、事件、发布、底部菜单仍需手动配置。",
            fg="#92400e",
            wraplength=850,
            justify=LEFT,
        ).pack(anchor="w")

        actions = Frame(root, padx=18, pady=8)
        actions.pack(fill=X)
        self.run_button = Button(actions, text="开始检查并配置", command=self._start, height=2, bg="#2563eb", fg="white")
        self.run_button.pack(side=LEFT)
        Button(actions, text="打开报告", command=self._open_report, height=2).pack(side=LEFT, padx=10)
        Button(actions, text="打开配置目录", command=self._open_config_dir, height=2).pack(side=LEFT)

        log_frame = Frame(root, padx=18, pady=10)
        log_frame.pack(fill=BOTH, expand=True)
        Label(log_frame, text="运行日志", font=("Arial", 14, "bold")).pack(anchor="w")
        self.log_box = Text(log_frame, height=20, wrap="word")
        self.log_box.pack(fill=BOTH, expand=True, pady=(6, 0))

    def _row(self, parent: Frame, label: str, var: StringVar, chooser, secret: bool = False) -> None:
        row = Frame(parent, pady=5)
        row.pack(fill=X)
        Label(row, text=label, width=24, anchor="w").pack(side=LEFT)
        entry = Entry(row, textvariable=var, show="*" if secret else "", width=72)
        entry.pack(side=LEFT, fill=X, expand=True)
        if chooser:
            Button(row, text="选择", command=chooser).pack(side=RIGHT, padx=(8, 0))

    def _choose_install_dir(self) -> None:
        chosen = filedialog.askdirectory(initialdir=str(Path(self.install_dir.get()).expanduser().parent))
        if chosen:
            self.install_dir.set(chosen)

    def _choose_work_dir(self) -> None:
        chosen = filedialog.askdirectory(initialdir=str(Path(self.work_dir.get()).expanduser()))
        if chosen:
            self.work_dir.set(chosen)

    def _start(self) -> None:
        if not self.app_id.get().strip() or not self.app_secret.get().strip():
            if not messagebox.askyesno("飞书信息未填完整", "App ID 或 App Secret 为空。继续运行只会生成不完整配置，确定继续吗？"):
                return
        self.run_button.config(state="disabled", text="正在运行...")
        self.results = []
        self.logs = []
        self.log_box.delete("1.0", END)
        threading.Thread(target=self._run_all, daemon=True).start()

    def _run_all(self) -> None:
        try:
            self._append("开始检查和配置...\n")
            self._check_commands()
            self._prepare_repo()
            self._build_project()
            self._write_config()
            self._add_manual_items()
            self._write_report()
            self._append(f"\n报告已生成：{REPORT_FILE}\n")
            webbrowser.open(REPORT_FILE.as_uri())
            self._finish("完成，报告已打开")
        except Exception as exc:
            self.results.append(Result("运行过程异常", "fail", f"{exc}\n\n{traceback.format_exc()}"))
            try:
                self._write_report()
                webbrowser.open(REPORT_FILE.as_uri())
            except Exception:
                pass
            self._append(f"\n发生错误：{exc}\n")
            self._finish("运行失败，已生成报告")

    def _finish(self, text: str) -> None:
        self.root.after(0, lambda: self.run_button.config(state="normal", text="开始检查并配置"))
        self.root.after(0, lambda: messagebox.showinfo("sy-feishu-connect", text))

    def _append(self, text: str) -> None:
        self.logs.append(text)
        def inner() -> None:
            self.log_box.insert(END, text)
            self.log_box.see(END)
        self.root.after(0, inner)

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
        self._check_one("make", "macOS 可执行：xcode-select --install", ["make", "--version"])
        self._check_one("codex", "请先安装并登录 Codex CLI，确保终端能运行 codex。", ["codex", "--version"])
        if any(r.status == "fail" and r.name.startswith("检查 ") for r in self.results):
            raise RuntimeError("环境检查未通过，请先安装缺失工具。")

    def _prepare_repo(self) -> None:
        self._append("\n== 2. 下载或更新 sy-feishu-connect ==\n")
        install_dir = Path(self.install_dir.get()).expanduser()
        if (install_dir / ".git").exists():
            code, out = self._run_cmd(["git", "pull", "--ff-only"], cwd=install_dir, timeout=180)
            status = "ok" if code == 0 else "fail"
            self.results.append(Result("更新源码", status, out.strip(), "git pull --ff-only"))
            if code != 0:
                raise RuntimeError("源码更新失败。")
        elif install_dir.exists() and any(install_dir.iterdir()):
            self.results.append(Result("下载源码", "fail", f"目录已存在且不是空目录：{install_dir}"))
            raise RuntimeError("安装目录已存在且不是空目录。")
        else:
            install_dir.parent.mkdir(parents=True, exist_ok=True)
            code, out = self._run_cmd(["git", "clone", REPO_URL, str(install_dir)], timeout=300)
            status = "ok" if code == 0 else "fail"
            self.results.append(Result("下载源码", status, out.strip(), f"git clone {REPO_URL}"))
            if code != 0:
                raise RuntimeError("源码下载失败。")

    def _build_project(self) -> None:
        self._append("\n== 3. 编译程序 ==\n")
        install_dir = Path(self.install_dir.get()).expanduser()
        code, out = self._run_cmd(["make", "build"], cwd=install_dir, timeout=600)
        binary = install_dir / "bin" / "sy-feishu-codex"
        if code == 0 and binary.exists():
            self.results.append(Result("编译 sy-feishu-codex", "ok", f"已生成：{binary}", "make build"))
        else:
            self.results.append(Result("编译 sy-feishu-codex", "fail", out.strip(), "make build"))
            raise RuntimeError("编译失败。")

    def _write_config(self) -> None:
        self._append("\n== 4. 生成配置文件 ==\n")
        project_name = self.project_name.get().strip() or "my-project"
        work_dir = Path(self.work_dir.get()).expanduser()
        app_id = self.app_id.get().strip()
        app_secret = self.app_secret.get().strip()
        install_dir = Path(self.install_dir.get()).expanduser()
        config_file = install_dir / "config.toml"

        if not work_dir.exists():
            self.results.append(Result("检查 work_dir", "fail", f"目录不存在：{work_dir}"))
            raise RuntimeError("work_dir 不存在。")
        self.results.append(Result("检查 work_dir", "ok", f"项目目录存在：{work_dir}"))

        if config_file.exists():
            backup = config_file.with_suffix(".toml.bak." + _dt.datetime.now().strftime("%Y%m%d-%H%M%S"))
            shutil.copy2(config_file, backup)
            self.results.append(Result("备份旧配置", "warn", f"旧配置已备份到：{backup}"))

        data_dir = install_dir / "data"
        content = f'''# Generated by sy-feishu-connect 配置向导

[feishu]
app_id = "{app_id}"
app_secret = "{app_secret}"
domain = "feishu"
require_mention = true
allow_users = "*"
allow_chats = "*"
working_emoji = "OnIt"
done_emoji = "DONE"

[codex]
work_dir = "{work_dir}"
cli_path = "codex"
model = ""
reasoning_effort = ""
mode = "suggest"
codex_home = ""
turn_timeout = "30m"

[bridge]
data_dir = "{data_dir}"
queue_messages = true
max_reply_chars = 3500

[log]
level = "info"
'''
        config_file.write_text(content, encoding="utf-8")
        masked = app_secret[:3] + "***" + app_secret[-3:] if len(app_secret) >= 8 else "***"
        self.results.append(Result("生成配置文件", "ok", f"配置文件：{config_file}\n项目名称：{project_name}\nApp ID：{app_id or '(未填写)'}\nApp Secret：{masked}"))
        self._append(f"✅ 配置文件已生成：{config_file}\n")

    def _add_manual_items(self) -> None:
        self.results.extend([
            Result("飞书后台：创建企业自建应用", "warn", "需要用户手动确认。路径：飞书开放平台 -> 开发者后台 -> 创建企业自建应用。"),
            Result("飞书后台：启用机器人", "warn", "需要用户手动确认。路径：应用能力 -> 机器人。"),
            Result("飞书后台：添加权限并发布", "warn", "至少添加 im:message.p2p_msg:readonly、im:message.group_at_msg:readonly、im:message:send_as_bot。"),
            Result("飞书后台：事件与回调", "warn", "事件 im.message.receive_v1；回调 card.action.trigger；订阅方式都选长连接。"),
            Result("飞书后台：底部自定义栏", "warn", "推荐 4 个菜单：会话、执行、设置、显示。具体见报告。"),
        ])

    def _write_report(self) -> None:
        REPORT_DIR.mkdir(parents=True, exist_ok=True)
        ok_count = sum(1 for r in self.results if r.status == "ok")
        fail_count = sum(1 for r in self.results if r.status == "fail")
        warn_count = sum(1 for r in self.results if r.status == "warn")
        generated_at = _dt.datetime.now().strftime("%Y-%m-%d %H:%M:%S")

        rows = "\n".join(
            f"<tr class='{html.escape(r.status)}'><td>{r.icon}</td><td>{html.escape(r.name)}</td><td><pre>{html.escape(r.detail)}</pre></td></tr>"
            for r in self.results
        )
        logs = html.escape("".join(self.logs)[-30000:])

        report = f"""<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>sy-feishu-connect 配置检查与飞书待办报告</title>
<style>
body{{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",Arial,sans-serif;margin:0;background:#eef3f8;color:#172033;line-height:1.6}}
.wrap{{max-width:1080px;margin:0 auto;background:white;min-height:100vh;padding:36px 46px;box-shadow:0 18px 50px rgba(23,32,51,.12)}}
h1{{margin:0 0 8px;font-size:36px}} h2{{margin-top:34px}}
.summary{{display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin:22px 0}}
.box{{border:1px solid #d8e2ee;border-radius:8px;padding:16px;background:#fbfdff}}
.num{{font-size:34px;font-weight:800}}
table{{width:100%;border-collapse:collapse;margin-top:14px}} th,td{{border:1px solid #d8e2ee;padding:10px;vertical-align:top;text-align:left}} th{{background:#f3f7fb}}
td:first-child{{font-size:22px;width:52px;text-align:center}}
pre{{white-space:pre-wrap;margin:0;font-family:"SFMono-Regular",Consolas,monospace;font-size:13px}}
.ok td{{background:#f3fbf5}} .fail td{{background:#fff5f5}} .warn td{{background:#fff8ed}}
.menu{{display:grid;grid-template-columns:repeat(2,1fr);gap:12px}}
.card{{border:1px solid #d8e2ee;border-radius:8px;padding:14px;background:#fbfdff}}
.log{{background:#0f172a;color:#dbeafe;border-radius:8px;padding:14px;max-height:420px;overflow:auto}}
code{{background:#eef4ff;color:#1d4ed8;padding:2px 5px;border-radius:5px}}
</style>
</head>
<body><div class="wrap">
<h1>sy-feishu-connect 配置检查与飞书待办报告</h1>
<p>生成时间：{html.escape(generated_at)}</p>
<div class="summary">
  <div class="box"><div class="num">✅ {ok_count}</div><div>自动检查通过</div></div>
  <div class="box"><div class="num">⚠️ {warn_count}</div><div>需要人工确认</div></div>
  <div class="box"><div class="num">❌ {fail_count}</div><div>失败项</div></div>
</div>
<h2>检查结果</h2>
<table><thead><tr><th>状态</th><th>项目</th><th>详情</th></tr></thead><tbody>{rows}</tbody></table>
<h2>推荐飞书底部自定义栏</h2>
<div class="menu">
  <div class="card"><h3>1. 会话</h3><p>新建会话 <code>/new</code><br>会话列表 <code>/sessions</code><br>当前会话 <code>/status</code></p></div>
  <div class="card"><h3>2. 执行</h3><p>停止执行 <code>/stop</code><br>当前状态 <code>/status</code><br>工作目录 <code>/pwd</code></p></div>
  <div class="card"><h3>3. 设置</h3><p>模式 <code>/mode</code><br>模型 <code>/model</code><br>帮助 <code>/help</code></p></div>
  <div class="card"><h3>4. 显示</h3><p>显示思考 <code>/display full</code><br>关闭思考 <code>/display compact</code><br>极简模式 <code>/display quiet</code></p></div>
</div>
<h2>结论和下一步</h2>
<p>如果失败项是 0，说明本机自动检查基本通过。⚠️ 是必须去飞书后台手动确认的项目，不代表程序失败。确认后可以直接双击仓库根目录里的 <code>双击启动机器人.command</code>。也可以用命令行启动：</p>
<pre>cd {html.escape(str(Path(self.install_dir.get()).expanduser()))}
./bin/sy-feishu-codex -config config.toml</pre>
<h2>运行日志</h2>
<pre class="log">{logs}</pre>
</div></body></html>"""
        REPORT_FILE.write_text(report, encoding="utf-8")

    def _open_report(self) -> None:
        if REPORT_FILE.exists():
            webbrowser.open(REPORT_FILE.as_uri())
        else:
            messagebox.showwarning("没有报告", "还没有生成报告，请先点击“开始检查并配置”。")

    def _open_config_dir(self) -> None:
        install_dir = Path(self.install_dir.get()).expanduser()
        install_dir.mkdir(parents=True, exist_ok=True)
        webbrowser.open(install_dir.as_uri())

    def run(self) -> None:
        self.root.mainloop()


if __name__ == "__main__":
    app = SetupApp()
    app.run()
