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

飞书后台配置完成后启动：

```bash
sy-feishu-connect start
```

## 飞书后台必须手动做

进入 [飞书开放平台](https://open.feishu.cn/app)，创建企业自建应用，然后完成：

1. 启用机器人能力。
2. 在「凭据与基础信息」复制 `App ID` 和 `App Secret`。
3. 在「权限管理」添加消息权限并发布。
4. 在「事件与回调」选择长连接，订阅 `im.message.receive_v1`。
5. 在「机器人自定义菜单」配置底部自定义栏。

更小白的图文步骤见 [使用教程.md](./使用教程.md) 和 [小白图文教程.html](./小白图文教程.html)。

## 推荐权限

| 权限标识 | 用途 |
| --- | --- |
| `im:message.p2p_msg:readonly` | 接收用户发给机器人的单聊消息 |
| `im:message.group_at_msg:readonly` | 接收群聊里 @机器人的消息 |
| `im:message:send_as_bot` | 以机器人身份回复消息 |
| `im:message:reaction` | 可选，给消息添加处理中/完成表情 |

## 飞书底部自定义栏推荐

| 分组 | 菜单项 |
| --- | --- |
| 会话 | 新建会话 `/new`、会话列表 `/sessions`、当前会话 `/status` |
| 执行 | 停止执行 `/stop`、当前状态 `/status`、工作目录 `/pwd` |
| 设置 | 模式 `/mode`、模型 `/model`、帮助 `/help` |
| 显示 | 显示思考 `/display full`、关闭思考 `/display compact`、极简模式 `/display quiet` |

当前已实现：`/help`、`/new`、`/status`、`/sessions`、`/stats`、`/whoami`、`/reset`。其余菜单项可以先作为产品入口预留。

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

如果在 `sy-feishu-connect setup` 里填写了统计上报地址，服务会把每次使用事件 `POST` 到该 HTTP 地址。推荐用 n8n webhook、自己的 API、日志收集服务或 serverless function 接收，再写入表格/数据库。普通用户不需要、也不应该推 GitHub main。

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
