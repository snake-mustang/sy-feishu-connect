# sy-feishu-connect

一个精简的飞书/Lark 到本机 Codex CLI 的远程桥接服务。它参考了 [chenhg5/cc-connect](https://github.com/chenhg5/cc-connect) 的设计目标：把运行在你机器上的 AI Agent 桥接到日常聊天工具里；本项目只保留“飞书消息 -> 本机 Codex -> 飞书回复”这一条线。

## 新手推荐：双击使用

如果你不熟悉命令行，下载仓库后按这个顺序来：

第一次配置时，双击根目录里的：

```text
双击打开配置工具.command
```

它会打开图形化配置向导，帮你检查 Codex、Git、Go、Make，编译程序，生成 `config.toml`，并在结束后自动打开「配置检查与飞书待办报告」。

以后每天启动机器人时，双击根目录里的：

```text
双击启动机器人.command
```

启动后会出现一个终端窗口。不要关闭它；窗口关闭后，飞书机器人就会停止。

你仍然需要手动去飞书开放平台完成：创建应用、启用机器人、添加权限、配置事件回调、发布应用、配置底部自定义栏。

详细图文版见：[小白图文教程.html](./小白图文教程.html)。

## 功能

- 飞书/Lark WebSocket 长连接，无需公网回调地址。
- 私聊直接发送任务，群聊默认需要 @机器人。
- 基于飞书 `open_id` / `chat_id` 的用户和群白名单。
- 每个飞书聊天保存独立 Codex `thread_id`，支持重启后续聊。
- 支持 `/new`、`/status`、`/sessions`、`/help` 远程控制命令。
- 通过 `codex exec --json` 调用本机 Codex CLI，支持 `suggest`、`auto-edit`、`yolo` 三种权限模式。

## 准备

1. 安装并登录 Codex CLI，确认本机可运行：

   ```bash
   codex exec --help
   ```

2. 创建飞书企业自建应用，启用机器人能力，并订阅事件：

   - `im.message.receive_v1`

3. 开启事件订阅的 WebSocket/长连接模式，并确保应用有发送消息权限。常用权限包括：

   - `im:message`
   - `im:message:send_as_bot`
   - `im:message:reaction`

完整飞书开放平台配置步骤见 [飞书/Lark 接入指南](./docs/feishu.md)。

## 配置

复制配置文件：

```bash
cp config.example.toml config.toml
```

填写：

```toml
[feishu]
app_id = "cli_xxx"
app_secret = "xxx"
domain = "feishu"
require_mention = true
allow_users = "*"
allow_chats = "*"

[codex]
work_dir = "/path/to/your/repo"
mode = "suggest"
```

权限模式：

- `suggest`: Codex 只读沙箱，适合资料研究、代码审查、问答。
- `auto-edit`: Codex 可写工作区，适合让它改代码；审批策略为 `never`，因为聊天端没有交互式审批 IPC。
- `yolo`: 跳过审批和沙箱，只适合你完全信任的隔离机器。

## 运行

```bash
make build
./bin/sy-feishu-codex -config config.toml
```

开发运行：

```bash
make run
```

## 飞书内使用

私聊机器人：

```text
帮我审查这个仓库的测试覆盖风险
```

群聊：

```text
@机器人 总结最近 5 个 commit 的主要变化
```

命令：

```text
/help
/new
/status
/sessions
```

## 安全建议

- 生产使用时不要把 `allow_users` 和 `allow_chats` 都设置成 `*`。
- 默认使用 `suggest`，确认工作目录和权限后再开启 `auto-edit`。
- 如果使用 `yolo`，建议把服务跑在隔离用户或隔离机器上。
- `config.toml` 包含飞书密钥，已被 `.gitignore` 忽略。

## 与 cc-connect 的关系

cc-connect 是一个完整的多平台、多 Agent 桥接项目。本项目基于它的目标和关键设计做了单线蒸馏：

- 只保留 Feishu/Lark 平台。
- 只保留 Codex CLI Agent。
- 去掉 TUI、cron、多平台 relay、附件处理、交互卡片和多模型供应商管理。

上游 README 标注为 MIT License；本项目代码为重新实现的精简版本。
