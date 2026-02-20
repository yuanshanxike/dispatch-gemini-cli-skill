# ACP Bridge for OpenClaw

通过 ACP 协议实现与 Gemini CLI 的多轮对话桥接。本 skill 提供一个 HTTP server，
OpenClaw agent 可通过 HTTP 请求创建、使用和管理与 Gemini CLI 的多轮对话
session。

---

## 前置条件

### 1. Go 环境

需要 Go 1.21+。验证：

```bash
go version
# 如未安装：https://go.dev/dl/
```

### 2. Gemini CLI

需要安装 Gemini CLI 并确保在 PATH 中可用：

```bash
# 安装
npm install -g @anthropic-ai/gemini-cli

# 验证
gemini --version
```

> 如果 Gemini CLI 安装在非标准路径，启动时通过 `--gemini-bin` 指定。

### 3. Gemini API Key

Gemini CLI 需要配置 API Key，确保你已完成 `gemini auth` 或设置了对应的环境变量。

---

## 部署与启动

### 一键部署

```bash
# 1. 进入项目目录
cd acp-bridge

# 2. 编译
go build -o acp-bridge .

# 3. 启动（默认端口 9090）
./acp-bridge --port 9090 --gemini-bin gemini
```

启动成功后你会看到：

```
level=INFO msg="Gemini CLI found" path=/usr/local/bin/gemini
level=INFO msg="ACP Bridge server starting" addr=:9090
```

### 后台运行（生产环境）

```bash
# 使用 nohup 后台运行
nohup ./acp-bridge --port 9090 --gemini-bin gemini > bridge.log 2>&1 &

# 检查是否运行正常
curl http://localhost:9090/health
# 预期输出：{"ok":true}
```

### 使用 systemd（Linux 服务器）

```ini
# /etc/systemd/system/acp-bridge.service
[Unit]
Description=ACP Bridge for OpenClaw
After=network.target

[Service]
ExecStart=/path/to/acp-bridge --port 9090 --gemini-bin /usr/local/bin/gemini
Restart=on-failure
RestartSec=5
Environment=HOME=/root

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable acp-bridge
sudo systemctl start acp-bridge
sudo systemctl status acp-bridge
```

---

## CLI 参数

| Flag             | Default   | 说明                                    |
| ---------------- | --------- | --------------------------------------- |
| `--port`         | `9090`    | HTTP server 端口                        |
| `--gemini-bin`   | `gemini`  | Gemini CLI binary 路径                  |
| `--gemini-model` | _(empty)_ | 指定 Gemini 模型（如 `gemini-2.5-pro`） |
| `--debug`        | `false`   | 启用 debug 日志                         |

---

## API 文档

### POST /session/create

创建一个 ACP session（启动 Gemini CLI 子进程）。

```json
// Request
{
  "cwd": "/path/to/workdir",
  "permissionCallbackUrl": "http://127.0.0.1:8080/permission/callback"
}

// Response
{ "sessionId": "sess-12345" }
```

- `cwd`（必填）：Gemini agent 工作的目录
- `permissionCallbackUrl`（推荐）：权限回调地址。未配置时，所有权限请求将被拒绝

### POST /session/prompt

发送一条 prompt 到已有 session（支持多轮，同一 session 保持上下文）。

```json
// Request
{ "sessionId": "sess-12345", "prompt": "分析这段代码的性能问题" }

// Response
{
  "text": "agent 输出的完整文本",
  "toolCalls": [
    { "toolCallId": "tc_1", "title": "ReadTextFile", "status": "completed" }
  ],
  "stopReason": "end_turn"
}
```

### POST /session/close

关闭 session 并终止 Gemini CLI 子进程。**用完务必关闭，否则子进程会残留。**

```json
// Request
{ "sessionId": "sess-12345" }

// Response
{ "ok": true }
```

### GET /health

健康检查。

```json
{ "ok": true }
```

---

## 权限回调

当 Gemini agent 需要执行敏感操作时，bridge 会 POST 到 `permissionCallbackUrl`。
此外，对敏感路径的文件写入（如 `/etc`、`~/.ssh` 等）也会触发额外的权限回调。

```json
// Bridge -> OpenClaw (POST permissionCallbackUrl)
{
  "sessionId": "sess-12345",
  "toolCallTitle": "WriteTextFile: /tmp/output.txt",
  "options": [
    { "optionId": "opt_1", "name": "Allow Once", "kind": "allow_once" },
    { "optionId": "opt_2", "name": "Deny", "kind": "deny" }
  ]
}

// 批准:
{ "selectedOptionId": "opt_1" }

// 拒绝:
{ "cancelled": true }
```

---

## OpenClaw Agent 集成指南

在 OpenClaw agent 的 tool 配置中，添加以下 HTTP 工具即可：

### Tool 1: gemini_create_session

```yaml
name: gemini_create_session
description: "创建一个 Gemini 多轮对话 session"
method: POST
url: "http://<bridge-host>:9090/session/create"
body:
    cwd: string # 工作目录
    permissionCallbackUrl: string # 权限回调 URL
returns: sessionId
```

### Tool 2: gemini_prompt

```yaml
name: gemini_prompt
description: "向 Gemini session 发送 prompt（多轮对话）"
method: POST
url: "http://<bridge-host>:9090/session/prompt"
body:
    sessionId: string # 从 gemini_create_session 获取
    prompt: string # 用户指令
returns: text, toolCalls, stopReason
```

### Tool 3: gemini_close_session

```yaml
name: gemini_close_session
description: "关闭 Gemini session，释放资源"
method: POST
url: "http://<bridge-host>:9090/session/close"
body:
    sessionId: string
returns: ok
```

### Agent 使用流程

```
1. 调用 gemini_create_session，获取 sessionId
2. 调用 gemini_prompt（可多轮），每次传入同一个 sessionId
3. 任务完成后调用 gemini_close_session 释放资源
```

---

## 多轮对话示例（curl）

```bash
# 1. 创建 session
ID=$(curl -s -X POST http://localhost:9090/session/create \
  -H 'Content-Type: application/json' \
  -d '{"cwd": "/my/project"}' | jq -r '.sessionId')

# 2. 第一轮 prompt
curl -s -X POST http://localhost:9090/session/prompt \
  -H 'Content-Type: application/json' \
  -d "{\"sessionId\": \"$ID\", \"prompt\": \"分析 main.go 的代码结构\"}"

# 3. 第二轮 prompt（保持上下文）
curl -s -X POST http://localhost:9090/session/prompt \
  -H 'Content-Type: application/json' \
  -d "{\"sessionId\": \"$ID\", \"prompt\": \"针对你发现的问题给出优化建议\"}"

# 4. 关闭 session
curl -s -X POST http://localhost:9090/session/close \
  -H 'Content-Type: application/json' \
  -d "{\"sessionId\": \"$ID\"}"
```

---

## 故障排查

| 症状                                           | 原因                                   | 解决方案                                |
| ---------------------------------------------- | -------------------------------------- | --------------------------------------- |
| 启动时 `❌ Error: Gemini CLI binary not found` | Gemini CLI 未安装或不在 PATH           | 安装 CLI 或使用 `--gemini-bin` 指定路径 |
| `ACP initialize failed`                        | Gemini CLI 不支持 `--experimental-acp` | 升级到支持 ACP 的版本                   |
| `session not found`                            | sessionId 错误或 session 已关闭        | 检查 sessionId 是否正确                 |
| 权限请求被拒绝                                 | 未配置 `permissionCallbackUrl`         | 创建 session 时传入回调 URL             |
| prompt 请求超时                                | Gemini agent 执行任务耗时较长          | 增大 HTTP client 超时时间               |
