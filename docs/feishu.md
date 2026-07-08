# 飞书/Lark 接入指南

本文档说明如何把 `sy-feishu-connect` 接入飞书，让飞书机器人调用本机 Codex CLI。

## 安装和检查

普通用户优先使用 npm 全局安装：

```bash
npm install -g https://github.com/snake-mustang/sy-feishu-connect/archive/refs/heads/main.tar.gz
```

检查是否可用：

```bash
sy-feishu-connect doctor
```

生成配置：

```bash
sy-feishu-connect setup
```

启动服务：

```bash
sy-feishu-connect start
```

默认配置文件在：

```text
~/.sy-feishu-connect/config.toml
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

打开 [飞书开放平台](https://open.feishu.cn/app)，创建「企业自建应用」。

建议：

| 字段 | 示例 |
| --- | --- |
| 应用名称 | `Codex 助手` |
| 应用描述 | `通过飞书远程操控本机 Codex` |

企业环境里可能需要管理员审批发布。

## 第二步：获取 App ID 和 App Secret

进入应用详情页：

1. 左侧点击「凭据与基础信息」。
2. 复制 `App ID` 和 `App Secret`。
3. 回到 `sy-feishu-connect setup` 填入。

推荐方式是运行配置向导：

```bash
sy-feishu-connect setup
```

它会依次问你「飞书 App ID」和「飞书 App Secret」，填完会自动写入：

```text
~/.sy-feishu-connect/config.toml
```

如果你要手动改配置文件，就打开这个文件，把 `[feishu]` 里的两行改成飞书后台复制出来的值：

```toml
[feishu]
app_id = "cli_xxxxxxxxxxxxx"
app_secret = "xxxxxxxxxxxxxxxxxxxxx"
domain = "feishu"
```

`App Secret` 不要提交到 Git，也不要发到聊天里。

## 第三步：启用机器人能力

路径：

```text
应用能力 -> 机器人
```

启用后，用户才能在飞书里搜索机器人或把机器人添加进群聊。

## 第四步：申请权限

进入「权限管理」，添加并申请发布以下权限。

### 推荐：批量导入权限

在飞书后台「权限管理」里，点击「批量处理」->「批量导入」，直接粘贴下面这段 JSON：

![飞书权限批量导入示意图](./assets/feishu-permission-bulk-import.png)

```json
{
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
}
```

`im:message.group_msg` 是敏感权限。如果你只让群聊 @ 机器人时触发，可以删掉这一行后再导入。

### 必选权限

| 权限名称 | 权限标识 | 用途 |
| --- | --- | --- |
| 读取用户发给机器人的单聊消息 | `im:message.p2p_msg:readonly` | 接收私聊消息 |
| 获取群组中用户 @ 机器人消息 | `im:message.group_at_msg:readonly` | 接收群聊里 @ 机器人的消息 |
| 以应用身份发送群消息 | `im:message:send_as_bot` | 机器人回复用户 |

### 可选权限

| 权限名称 | 权限标识 | 用途 |
| --- | --- | --- |
| 获取与更新用户基本信息 | `contact:user.base:readonly` | 推荐，用于本机统计时尽量把 `open_id` 对应到飞书姓名 |
| 获取群组中所有消息 | `im:message.group_msg` | 敏感权限；仅当你关闭 @ 要求、希望读取群内普通消息时申请 |
| 添加消息表情回复 | `im:message:reaction` | 可选，用于处理中/完成表情 |

当前版本默认只处理私聊和群聊 @ 机器人消息，所以 `im:message.group_msg` 不是必选。它是敏感权限，能不申请就先不申请。

权限变更后需要创建新版本并发布，否则运行时仍可能提示权限不足。

## 第五步：配置事件订阅

进入「事件与回调」。

订阅方式选择：

```text
使用长连接接收事件
```

添加事件：

| 事件名称 | 事件标识 | 用途 |
| --- | --- | --- |
| 接收消息 | `im.message.receive_v1` | 接收用户发送给机器人的消息 |
| 机器人自定义菜单事件 | `application.bot.menu_v6` | 接收底部菜单“推送事件”点击 |

保存后创建版本并发布。

`application.bot.menu_v6` 必须添加。少了它，用户点底部菜单时飞书不会把菜单事件发给本机长连接。

## 第六步：配置底部自定义栏

飞书后台路径通常是：

```text
应用能力 -> 机器人 -> 机器人自定义菜单
```

推荐按 4 组来设计，用户更容易理解。每个菜单项都设置成：

```text
响应动作：推送事件
事件 ID：下面表格里的固定值
```

| 分组 | 菜单项 | 推送事件 ID | 实际执行 |
| --- | --- | --- | --- |
| 会话 | 新建会话 | `session_new` | `/new` |
| 会话 | 会话列表 | `session_list` | `/sessions` |
| 会话 | 当前会话 | `session_current` | `/status` |
| 执行 | 停止执行 | `exec_stop` | `/stop` |
| 执行 | 当前状态 | `exec_status` | `/status` |
| 执行 | 工作目录 | `exec_workdir` | `/pwd` |
| 设置 | 模式 | `settings_mode` | `/mode` |
| 设置 | 模型 | `settings_model` | `/model` |
| 设置 | 帮助 | `settings_help` | `/help` |
| 显示 | 显示思考（默认） | `display_thinking_on` | `/display thinking` |
| 显示 | 关闭思考 | `display_thinking_off` | `/display final` |
| 显示 | 极简模式 | `display_minimal` | `/display quiet` |

默认会显示 Codex 的执行过程和工具进度；只想看最终结果时，再点「关闭思考」或发送 `/display final`。

如果菜单点了没反应，先检查两件事：事件里有没有订阅 `application.bot.menu_v6`，应用有没有发布新版本。

当前已支持 `/help`、`/new`、`/status`、`/sessions`、`/stop`、`/pwd`、`/mode`、`/model`、`/display`、`/stats`、`/whoami`、`/reset`。

## 第七步：启动服务

飞书后台发布完成后：

```bash
sy-feishu-connect start
```

启动成功后保持终端窗口打开。窗口关闭，机器人就会停止。

## 飞书内命令

| 命令 | 作用 |
| --- | --- |
| `/help` | 查看帮助 |
| `/new` | 当前聊天开启新的 Codex 会话 |
| `/status` | 查看当前聊天绑定的 Codex `thread_id` |
| `/sessions` | 列出最近保存的会话 |
| `/stats` | 查看使用统计 |
| `/whoami` | 查看当前用户的飞书用户标识，方便统计时对应真人 |
| `/reset` | 重置当前聊天会话，等同于重新开始 |

普通消息会直接发送给 Codex。

## 使用统计

本地会保存：

```text
usage_events.jsonl
usage_summary.json
```

飞书里发送 `/stats` 可以快速查看 Top 用户、总消息数、成功失败次数。飞书里发送 `/whoami` 可以看到自己的 `open_id`、当前聊天 ID 和聊天类型，方便管理员对应真实姓名。

如果飞书后台已申请 `contact:user.base:readonly` 权限并发布应用，服务会尽量把发送消息的人解析成飞书姓名。如果暂时拿不到姓名，统计仍会保留 `open_id`，后续可以用 `/whoami` 人工对应。

如需公司集中统计，建议由管理员预置 `SY_FEISHU_CONNECT_REPORT_URL`。首次配置成功和后续每次使用，只会 POST 一条极简 JSON：

```json
{"姓名":"张三","是否成功":true}
```

普通用户不需要推 GitHub，也不应该拥有写 GitHub main 的权限。

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

## 常见问题

### 消息没有响应

检查：

1. `sy-feishu-connect start` 是否仍在运行。
2. 飞书事件订阅是否选择了「使用长连接接收事件」。
3. 是否订阅了 `im.message.receive_v1`。
4. 应用版本是否已经发布。
5. 群聊里是否 @ 了机器人。
6. `allow_users` / `allow_chats` 是否拦截了消息。

### Codex 启动失败

在同一终端里检查：

```bash
codex --version
codex exec --help
```

如果服务由后台进程管理器启动，注意它的 `PATH` 可能和交互式终端不同。可以在配置里写绝对路径：

```toml
[codex]
cli_path = "/Users/admin/.local/bin/codex"
```

### 想让 Codex 修改代码

把模式改为：

```toml
[codex]
mode = "auto-edit"
```

聊天端没有 Codex TUI 的交互式审批能力，请只对可信用户和可信项目开启。

### Lark 国际版怎么配

配置：

```toml
[feishu]
domain = "lark"
```

同时在 [Lark Open Platform](https://open.larksuite.com/) 创建应用并获取凭证。

## 参考链接

- [飞书开放平台](https://open.feishu.cn/)
- [Lark Open Platform](https://open.larksuite.com/)
- [飞书开放平台文档](https://open.feishu.cn/document/)
- [权限列表](https://open.feishu.cn/document/server-docs/application-scope/scope-list)
