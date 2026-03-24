# Gemini-CLI ACP Bridge for Claw

[English](./README.md) | [日本語](./README.ja.md)

---

## 简介

通过 [ACP (Agent Client Protocol)](https://github.com/coder/acp-go-sdk)
协议实现与 Gemini CLI 的多轮对话桥接。

本项目提供了一个 HTTP Server，使得 OpenClaw Agent 可以无缝创建、使用和管理与
Gemini CLI 的多轮对话 Session。

### 前置条件

1. **Gemini CLI**：需要安装 Gemini CLI，并确保在系统环境变量 `PATH` 中可用。
   ```bash
   npm install -g @google/gemini-cli
   ```
2. **Go 环境**（_可选_）：Go 1.21+（如果你想自己编译本项目）

### 快速启动

1. 访问 _Releases 页面_ ，根据你的系统下载对应的压缩包（macOS/Linux 为
   `.tar.gz`，Windows 为 `.zip`）。
2. 解压下载的压缩包。解压后的目录包含 `acp-bridge` 二进制文件和 `SKILL.md`
   文件。
3. 让你的 Agent 使用此 skill（服务器默认端口 9090）。

### 源码编译（可选）

如果你倾向于自己编译本项目：

```bash
# 克隆代码并进入目录
git clone git@github.com:yuanshanxike/dispatch-gemini-cli-skill.git

# 编译项目
go build -o acp-bridge .
```

### 命令行参数

| 参数             | 默认值   | 说明                                           |
| ---------------- | -------- | ---------------------------------------------- |
| `--port`         | `9090`   | HTTP server 端口                               |
| `--gemini-bin`   | `gemini` | Gemini CLI 的二进制文件路径                    |
| `--gemini-model` | _(空)_   | 指定使用的 Gemini 模型（例如：`gemini-3-pro`） |
| `--debug`        | `false`  | 启用 debug 日志记录                            |

### API 接口

- **`POST /session/create`**: 创建一个新的 ACP session 并在后台启动一个 Gemini
  CLI 子进程。
- **`POST /session/prompt`**: 发送一条 prompt 到已有的
  session（维持多轮对话上下文）。
- **`POST /session/close`**: 终止 session 并清理子进程。
- **`GET /health`**: 健康检查接口。

_关于 API payload 详细结构和 OpenClaw Agent 集成指南，可以参考 `SKILL.md`
文件。_
