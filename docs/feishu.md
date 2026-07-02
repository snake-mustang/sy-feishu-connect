# 飞书/Lark 接入指南

本文档说明如何把 `sy-feishu-connect` 接入飞书，让你通过飞书机器人远程操控本机 Codex CLI。

本项目是从 `cc-connect` 思路蒸馏出的单线版本：只支持 **Feishu/Lark WebSocket 长连接 + Codex CLI**，不需要公网 IP、域名或反向代理。

## 前置要求

- 一个飞书或 Lark 账号。
- 一台能长期运行本服务的电脑或服务器。
- 本机已安装并登录 Codex CLI：

  ```bash
  codex exec --help
  ```

- 已构建本项目：

  ```bash
  cd /Users/admin/sy/code/sy-feishu-connect
  make build
  ```

## 接入架构

```text
飞书客户端
   |
   v
飞书开放平台 WebSocket 长连接
   |
   v
sy-feishu-connect
   |
   v
codex exec --json
   |
   v
你的本地项目目录
```

## 第一步：创建飞书企业自建应用

1. 打开 [飞书开放平台](https://open.feishu.cn/)。
2. 进入「控制台」。
3. 创建「企业自建应用」。
4. 填写应用名称、描述和图标。

建议：

| 字段 | 示例 |
| --- | --- |
| 应用名称 | `Codex Remote` |
| 应用描述 | `通过飞书远程操控本机 Codex` |

个人开发者也可以创建自建应用；企业环境里可能需要管理员审批发布。

## 第二步：获取 App ID 和 App Secret

进入应用详情页：

1. 左侧点击「凭据与基础信息」。
2. 复制 `App ID` 和 `App Secret`。

示例：

```text
App ID:     cli_xxxxxxxxxxxxxxxx
App Secret: xxxxxxxxxxxxxxxxxxxx
```

`App Secret` 不要提交到 Git，也不要发到聊天里。本项目默认忽略 `config.toml`。

## 第三步：启用机器人能力

1. 左侧点击「应用能力」。
2. 进入「机器人」。
3. 点击启用。
4. 配置机器人名称、描述和头像。

启用后，用户才能在飞书里搜索机器人或把机器人添加进群聊。

## 第四步：申请权限

进入「权限管理」，添加并申请发布以下权限。

最小建议权限：

| 权限标识 | 用途 |
| --- | --- |
| `im:message.p2p_msg:readonly` | 接收用户发给机器人的单聊消息 |
| `im:message.group_at_msg:readonly` | 接收群聊里 @机器人的消息 |
| `im:message:send_as_bot` | 以机器人身份回复消息 |

可选权限：

| 权限标识 | 用途 |
| --- | --- |
| `im:message:reaction` | 给用户消息添加处理中/完成表情 |
| `im:message.group_msg` | 如果你关闭 `require_mention` 并希望接收群内普通消息，可能需要该敏感权限 |

本项目当前只处理文本和富文本消息，不需要文件、图片、语音、卡片回调等权限。

权限变更后需要创建新版本并发布，否则运行时仍可能提示权限不足。

## 第五步：配置事件订阅

进入「事件与回调」。

在「事件配置」中选择：

```text
使用长连接接收事件
```

添加事件：

| 事件标识 | 用途 |
| --- | --- |
| `im.message.receive_v1` | 接收用户消息 |

本项目不使用交互卡片，因此不需要配置 `card.action.trigger` 回调。

保存后创建版本并发布。

## 第六步：配置服务

复制配置文件：

```bash
cd /Users/admin/sy/code/sy-feishu-connect
cp config.example.toml config.toml
```

填写飞书凭证：

```toml
[feishu]
app_id = "cli_xxxxxxxxxxxxxxxx"
app_secret = "xxxxxxxxxxxxxxxxxxxx"
domain = "feishu"
require_mention = true
allow_users = "*"
allow_chats = "*"
working_emoji = "OnIt"
done_emoji = "DONE"
```

国内飞书使用：

```toml
domain = "feishu"
```

国际版 Lark 使用：

```toml
domain = "lark"
```

配置 Codex 工作目录：

```toml
[codex]
work_dir = "/Users/admin/sy/code/your-project"
cli_path = "codex"
mode = "suggest"
turn_timeout = "30m"
```

权限模式说明：

| 模式 | Codex 权限 | 适用场景 |
| --- | --- | --- |
| `suggest` | 只读沙箱 | 代码审查、资料研究、问答、数据分析 |
| `auto-edit` | 可写工作区，审批策略为 `never` | 允许 Codex 直接改当前项目 |
| `yolo` | 跳过审批和沙箱 | 仅限隔离机器或完全可信环境 |

桥接配置：

```toml
[bridge]
data_dir = "./data"
queue_messages = true
max_reply_chars = 3500
```

`data_dir` 会保存每个飞书聊天对应的 Codex `thread_id`，服务重启后可以继续同一会话。

## 第七步：启动服务

构建并启动：

```bash
make build
./bin/sy-feishu-codex -config config.toml
```

开发运行：

```bash
make run
```

启动成功后，日志里应该能看到：

```text
bridge started work_dir=/path/to/project
feishu sdk ...
```

如果能获取机器人身份，还会看到：

```text
feishu: bot identified open_id=...
```

## 第八步：添加机器人到会话

单聊：

1. 在飞书里搜索机器人名称。
2. 打开会话。
3. 直接发送任务。

群聊：

1. 打开目标群。
2. 进入群设置。
3. 添加机器人。
4. 默认需要 @机器人 才会触发 Codex。

示例：

```text
@Codex Remote 帮我审查这个仓库最近的改动风险
```

## 飞书内命令

| 命令 | 作用 |
| --- | --- |
| `/help` | 查看帮助 |
| `/new` | 当前聊天开启新的 Codex 会话 |
| `/status` | 查看当前聊天绑定的 Codex `thread_id` |
| `/sessions` | 列出最近保存的会话 |
| `/stats` | 查看按飞书用户标识汇总的使用统计 |
| `/whoami` | 查看当前用户的飞书用户标识，方便统计时对应真人 |
| `/reset` | 重置当前聊天会话，等同于重新开始 |

普通消息会直接发送给 Codex。

## 底部自定义栏推荐

飞书后台路径通常是：

```text
应用能力 -> 机器人 -> 机器人自定义菜单
```

推荐按 4 组来设计，用户更容易理解：

| 分组 | 菜单项 |
| --- | --- |
| 会话 | 新建会话 `/new`、会话列表 `/sessions`、当前会话 `/status` |
| 执行 | 停止执行 `/stop`、当前状态 `/status`、工作目录 `/pwd` |
| 设置 | 模式 `/mode`、模型 `/model`、帮助 `/help` |
| 显示 | 显示思考 `/display full`、关闭思考 `/display compact`、极简模式 `/display quiet` |

当前版本已支持 `/help`、`/new`、`/status`、`/sessions`、`/stats`、`/whoami`、`/reset`。其余菜单可以先作为推荐预留项，后续补齐命令实现；如果不想放预留项，可以只配置当前已支持的命令。

## 使用统计

服务会在本机 `data` 目录保存统计文件：

```text
usage_events.jsonl   每条消息一行，适合后续导入表格或脚本分析
usage_summary.json   按用户、群聊、命令汇总后的结果
```

飞书里发送 `/stats` 可以快速查看 Top 用户、总消息数、成功失败次数。飞书里发送 `/whoami` 可以看到自己的 `open_id`，方便你把统计结果和真实用户对应起来。

## 白名单配置

生产使用时建议不要用 `*`。

只允许指定用户：

```toml
[feishu]
allow_users = "ou_xxx,ou_yyy"
```

只允许指定群：

```toml
[feishu]
allow_chats = "oc_xxx,oc_yyy"
```

获取 `open_id` / `chat_id` 的简单方式：

1. 先临时设置 `allow_users = "*"`、`allow_chats = "*"`;
2. 启动服务并发送消息；
3. 查看日志中的用户和群相关字段；
4. 回填白名单后重启服务。

## 常见问题

### 消息没有响应

检查：

1. 服务是否正在运行。
2. 飞书事件订阅是否选择了「使用长连接接收事件」。
3. 是否订阅了 `im.message.receive_v1`。
4. 应用版本是否已经发布。
5. 群聊里是否 @ 了机器人。
6. `allow_users` / `allow_chats` 是否拦截了消息。

### 群聊不 @ 机器人也想触发

配置：

```toml
[feishu]
require_mention = false
```

注意：这会让机器人处理更多群消息，建议配合 `allow_chats` 使用，并确认应用拥有读取群消息所需权限。

### Codex 启动失败

在运行服务的同一终端里检查：

```bash
command -v codex
codex exec --help
```

如果服务由 `launchd`、`systemd` 或其他进程管理器启动，注意它的 `PATH` 可能和你的交互式 shell 不同。可以在配置里写绝对路径：

```toml
[codex]
cli_path = "/Users/admin/.local/bin/codex"
```

### Codex 提示不是 Git 仓库

本项目调用 Codex 时已加 `--skip-git-repo-check`，通常不会因为非 Git 目录失败。但仍建议把 `work_dir` 指向真实项目目录，便于 Codex 获取上下文。

### 想让 Codex 修改代码

把模式改为：

```toml
[codex]
mode = "auto-edit"
```

这会使用 `workspace-write` 沙箱和 `approval_policy=never`。聊天端没有 Codex TUI 的交互式审批能力，所以请只对可信用户和可信项目开启。

### 表情反应失败

如果没有 `im:message:reaction` 权限，表情反应会失败，但不影响 Codex 回复。可以关闭：

```toml
[feishu]
working_emoji = ""
done_emoji = ""
```

### Lark 国际版怎么配

配置：

```toml
[feishu]
domain = "lark"
```

同时在 [Lark Open Platform](https://open.larksuite.com/) 创建应用并获取凭证。

## 当前限制

- 只处理文本和富文本消息。
- 不处理图片、文件、语音、视频。
- 不使用飞书交互卡片。
- 不内置扫码创建应用流程，需要手动在开放平台创建应用。
- 一个服务实例绑定一个 Codex 工作目录；多项目可运行多个实例并使用不同配置。

## 参考链接

- [飞书开放平台](https://open.feishu.cn/)
- [Lark Open Platform](https://open.larksuite.com/)
- [飞书开放平台文档](https://open.feishu.cn/document/)
- [机器人开发指南](https://open.feishu.cn/document/ukTMukTMukTM/uYjNwUjL2YDM14iN2ATN)
- [事件订阅文档](https://open.feishu.cn/document/ukTMukTMukTM/uUTNz4SN1MjL1UzM)
- [权限列表](https://open.feishu.cn/document/server-docs/application-scope/scope-list)
