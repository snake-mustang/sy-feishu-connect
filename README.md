# sy-feishu-connect

把飞书/Lark 机器人连接到本机 Codex CLI。用户在飞书里发消息，本机 `sy-feishu-connect` 收到后交给 Codex 执行，再把结果回到飞书。

核心链路：

```text
飞书消息 -> sy-feishu-connect -> 本机 Codex CLI -> 你的项目目录 -> 飞书回复
```

## 新手只记 3 步

### 1. 安装

```bash
npm install -g https://github.com/snake-mustang/sy-feishu-connect/archive/refs/heads/main.tar.gz
```

以后如果发布到 npm 官方 registry，也可以改用更短的：

```bash
npm install -g sy-feishu-connect
```

### 2. 检查是否可用

```bash
sy-feishu-connect doctor
```

看到 Node.js、Codex CLI、sy-feishu-connect core 都是 `✅`，就可以继续。

### 3. 生成配置

```bash
sy-feishu-connect setup
```

它会让你填写：

- Codex 要操作的项目目录
- 飞书 App ID
- 飞书 App Secret
- 姓名/工号，用于统计谁在用
- 统计上报地址，可空

默认会生成：

```text
~/.sy-feishu-connect/config.toml
```

其中飞书凭证会写入：

```toml
[feishu]
app_id = "cli_xxxxxxxxxxxxx"
app_secret = "xxxxxxxxxxxxxxxxxxxxx"
```

飞书后台配置完成后启动：

```bash
sy-feishu-connect start
```

## 飞书后台必须手动做

进入 [飞书开放平台](https://open.feishu.cn/app)，创建企业自建应用，然后完成：

1. 启用机器人能力。
2. 在「凭据与基础信息」复制 `App ID` 和 `App Secret`。
3. 在「权限管理」用批量导入添加消息权限并发布。
4. 在「事件与回调」选择长连接，订阅 `im.message.receive_v1`。
5. 在「机器人自定义菜单」配置底部自定义栏，菜单动作统一选「发送文字消息」。

更小白的图文步骤见 [使用教程.md](./使用教程.md) 和 [小白图文教程.html](./小白图文教程.html)。

## 推荐权限

在飞书后台「权限管理」里，点击「批量处理」->「批量导入」，直接粘贴：

![飞书权限批量导入示意图](./docs/assets/feishu-permission-bulk-import.png)

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

必选：

| 权限名称 | 权限标识 | 用途 |
| --- | --- | --- |
| 读取用户发给机器人的单聊消息 | `im:message.p2p_msg:readonly` | 接收私聊消息 |
| 获取群组中用户 @ 机器人消息 | `im:message.group_at_msg:readonly` | 接收群聊 @ 消息 |
| 以应用身份发送群消息 | `im:message:send_as_bot` | 发送回复 |

可选：

| 权限名称 | 权限标识 | 用途 |
| --- | --- | --- |
| 获取与更新用户基本信息 | `contact:user.base:readonly` | 自动对应姓名/工号 |
| 获取群组中所有消息 | `im:message.group_msg` | 敏感权限；仅关闭 @ 要求时需要 |
| 添加消息表情回复 | `im:message:reaction` | 处理中/完成表情 |

事件配置里添加：

| 事件名称 | 事件标识 | 用途 |
| --- | --- | --- |
| 接收消息 | `im.message.receive_v1` | 接收用户发送的消息 |

底部菜单默认不需要订阅 `application.bot.menu_v6`。飞书后台里的「推送事件」会让飞书服务器向“请求地址”发 HTTP POST，本地长连接收不到这类点击；这个项目的小白路径请统一用「发送文字消息」。

## 飞书底部自定义栏推荐

每个菜单项都这样填：

```text
响应动作：发送文字消息
名称：下面表格里的中文菜单名
```

飞书没有单独的“发送内容”输入框，会把菜单「名称」作为消息发给机器人。下面这些中文名称已经内置映射：

| 分组 | 子菜单名称 | 实际执行 |
| --- | --- | --- |
| 会话 | 新建会话 | `/new` |
| 会话 | 会话列表 | `/sessions` |
| 会话 | 当前会话 | `/status` |
| 执行 | 停止执行 | `/stop` |
| 执行 | 当前状态 | `/status` |
| 执行 | 工作目录 | `/pwd` |
| 设置 | 模式 | `/mode` |
| 设置 | 模型 | `/model` |
| 设置 | 帮助 | `/help` |
| 显示 | 显示思考 | `/display full` |
| 显示 | 关闭思考 | `/display compact` |
| 显示 | 极简模式 | `/display quiet` |

不要选「推送事件」。如果选了它，飞书会把点击事件发到 HTTP 请求地址；你现在跑的是本机长连接，所以菜单点击会没反应。

当前已实现：`/help`、`/new`、`/status`、`/sessions`、`/stop`、`/pwd`、`/mode`、`/model`、`/display`、`/stats`、`/whoami`、`/reset`。

## 使用统计

本机会保存：

```text
data/usage_events.jsonl
data/usage_summary.json
```

飞书里可用：

```text
/stats
/whoami
```

如果飞书后台已申请 `contact:user.base:readonly` 并发布应用，统计会自动把 `open_id` 对应成飞书姓名/工号；如果暂时拿不到，也会保留 `open_id`，让用户发 `/whoami` 后可以人工对应。

如果在 `sy-feishu-connect setup` 里填写了统计上报地址，服务会把每次使用事件 `POST` 到该 HTTP 地址。事件里会包含 `user_id`、`feishu_user_name`、`feishu_employee_no`、命令、成功失败、耗时等字段。推荐用 n8n webhook、自己的 API、日志收集服务或 serverless function 接收，再写入表格/数据库。普通用户不需要、也不应该推 GitHub main。

## 安全建议

- `config.toml` 包含飞书密钥，不要提交到 Git。
- 生产使用不要长期保持 `allow_users = "*"` 和 `allow_chats = "*"`。
- 默认模式是 `suggest`，只读更稳；确认可信后再用 `auto-edit`。
- `yolo` 只适合隔离机器或完全可信环境。

## 开发

```bash
make build
make test
node cli/sy-feishu-connect.js doctor
npm pack --dry-run
```
