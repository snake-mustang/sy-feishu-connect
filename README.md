# sy-feishu-connect

把飞书/Lark 机器人连接到本机 Codex CLI。用户在飞书里发消息，本机 `sy-feishu-connect` 收到后交给 Codex 执行，再把结果回到飞书。

核心链路：

```text
飞书消息 -> sy-feishu-connect -> 本机 Codex CLI -> 你的项目目录 -> 飞书回复
```

## 新手只记 3 步

### 1. 安装

```bash
npm install -g https://github.com/snake-mustang/sy-feishu-connect/archive/f9e7e1a.tar.gz
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

如果你是从 GitHub 下载的完整文件夹，也可以双击根目录里的 `双击打开配置工具.command`（Mac）或 `双击打开配置工具.bat`（Windows）生成配置。

它会让你填写：

- Codex 工作目录，可以不填项目路径，不操作项目就用默认值
- 飞书 App ID
- 飞书 App Secret
- 姓名-中文，用于统计安装和使用成功率

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
3. 在「权限管理」用批量导入添加消息权限。
4. 在「事件与回调」选择长连接，只订阅 `im.message.receive_v1`。
5. 在「机器人自定义菜单」配置底部自定义栏，菜单动作统一选「发送文字」。
6. 到「版本管理与发布」创建版本并发布。

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
      "im:message:send_as_bot",
      "im:message:reaction"
    ],
    "user": []
  }
}
```

`im:message.group_msg` 是敏感权限。如果你只让群聊 @ 机器人时触发，可以删掉这一行后再导入。`im:message:reaction` 用于给单聊/群聊消息加处理中和完成表情；不需要表情时可以删掉。已按旧教程配置过的用户，需要补加 `im:message:reaction` 并重新发布应用。

必选：

| 权限名称 | 权限标识 | 用途 |
| --- | --- | --- |
| 读取用户发给机器人的单聊消息 | `im:message.p2p_msg:readonly` | 接收私聊消息 |
| 获取群组中用户 @ 机器人消息 | `im:message.group_at_msg:readonly` | 接收群聊 @ 消息 |
| 以应用身份发送群消息 | `im:message:send_as_bot` | 发送回复 |

可选：

| 权限名称 | 权限标识 | 用途 |
| --- | --- | --- |
| 获取与更新用户基本信息 | `contact:user.base:readonly` | 本机统计时尽量显示飞书姓名 |
| 获取群组中所有消息 | `im:message.group_msg` | 敏感权限；仅关闭 @ 要求时需要 |
| 添加消息表情回复 | `im:message:reaction` | 处理中/完成表情 |

事件配置里添加：

| 事件名称 | 事件标识 | 用途 |
| --- | --- | --- |
| 接收消息 | `im.message.receive_v1` | 接收用户发送的消息 |

底部菜单改用「发送文字」后，不需要再添加菜单事件。

## 飞书底部自定义栏推荐

每个菜单项都这样填：

```text
响应动作：发送文字
菜单名称：照抄下面表格里的菜单项
```

飞书这里通常没有“发送内容”输入框；它会把菜单名称当作文字发给机器人。工具已经内置下面这些中文菜单名称的识别。

| 分组 | 菜单项 | 实际执行 |
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
| 显示 | 显示思考（默认） | `/display thinking` |
| 显示 | 关闭思考 | `/display final` |
| 显示 | 极简模式 | `/display quiet` |

默认会显示 Codex 返回的可展示思考摘要、执行过程和工具进度；只想看最终结果时，再点「关闭思考」或发送 `/display final`。

注意：这里不是隐藏思维链。工具只转发 Codex CLI 实际返回的 `reasoning.summary` 和工具进度；如果当前 CLI 或模型网关只返回 `encrypted_content`，并且 `summary` 是空数组，就不会额外编造一段“思考”。

最终回答底部会自动附带模型、推理强度、token/context 占用和工作目录，方便复盘这次是谁、在哪个目录、用什么配置跑的。

如果你想尽量打开 Codex 的可展示思考摘要，可以在 `~/.codex/config.toml` 里尝试加入：

```toml
model_reasoning_summary = "detailed"
model_supports_reasoning_summaries = true
hide_agent_reasoning = false
show_raw_agent_reasoning = true
```

改完后重启 `sy-feishu-connect start`。如果仍然没有思考摘要，多半是当前 Codex CLI / 模型 / 自定义网关没有把 summary 透出；这时只能显示工具过程、最终结果和底部模型/token 信息。

如果菜单点了没反应，优先检查菜单动作是不是「发送文字」、菜单名称是不是上面的中文，以及应用有没有重新发布。

当前已实现：`/help`、`/new`、`/status`、`/sessions`、`/stop`、`/pwd`、`/mode`、`/model`、`/display`、`/stats`、`/whoami`、`/reset`。

## 使用统计

本机会保存：

```text
data/usage_events.jsonl
data/usage_summary.json
```

如果使用默认配置工具，实际路径通常是：

```text
~/.sy-feishu-connect/data/usage_events.jsonl
~/.sy-feishu-connect/data/usage_summary.json
```

飞书里可用：

```text
/stats
/whoami
```

如果飞书后台已申请 `contact:user.base:readonly` 并发布应用，统计会尽量把 `open_id` 对应成飞书姓名；如果暂时拿不到，也会保留 `open_id`，让用户发 `/whoami` 后可以人工对应真实姓名。

当前公司版已强制内置飞书群机器人统计 Webhook。首次配置成功和后续每次使用都会上报到飞书群，群里会收到：

```text
sy-feishu-connect 使用上报
姓名：张三
是否成功：是
```

该统计链路已验证可以成功接收。用户本机旧版本需要先重新安装，再运行 `sy-feishu-connect doctor` 触发核心程序更新。

飞书群机器人如果配置安全策略，建议先用关键词校验，并把关键词设为 `sy-feishu-connect`；这版暂不处理签名校验。

普通用户不需要推 GitHub，也不应该拥有写 GitHub main 的权限。

开发者注意：`npm install -g https://github.com/...` 本身不会提供“谁安装了”的明细后台；真正统计发生在用户运行配置工具和后续使用时。当前 Webhook 已写入代码并会覆盖用户本机 `report_url` / `SY_FEISHU_CONNECT_REPORT_URL`，如公开分发前需要先替换或移除这个地址。

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
