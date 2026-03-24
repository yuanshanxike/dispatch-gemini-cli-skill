# Gemini-CLI ACP Bridge for Claw

[中文](./README.zh.md) | [日本語](./README.ja.md)

---

## Introduction

A bridge that implements multi-turn conversation tunneling with the Gemini CLI
via the [Agent Client Protocol (ACP)](https://github.com/coder/acp-go-sdk).

This project provides an HTTP server that allows OpenClaw agents to create, use,
and manage multi-turn conversation sessions with the Gemini CLI seamlessly.

### Prerequisites

1. **Gemini CLI**: Must be installed and accessible in your system `PATH`.
   ```bash
   npm install -g @google/gemini-cli
   ```
2. **Go Environment** (_Optional_): Go 1.21+ (If you want to build the project
   yourself)

### Quick Start

1. Go to the _Releases page_ and download the appropriate package for your
   system (`.tar.gz` for macOS/Linux, `.zip` for Windows).
2. Extract the archive. The extracted directory contains the `acp-bridge` binary
   and the `SKILL.md` file.
3. Let your agent use this skill (server default port 9090).

### Build from Source (Optional)

If you prefer to build the project yourself:

```bash
# Clone and enter the directory
git clone git@github.com:yuanshanxike/dispatch-gemini-cli-skill.git

# Build the project
go build -o acp-bridge .
```

### Command Line Arguments

| Flag             | Default   | Description                                         |
| ---------------- | --------- | --------------------------------------------------- |
| `--port`         | `9090`    | HTTP server port                                    |
| `--gemini-bin`   | `gemini`  | Path to the Gemini CLI binary                       |
| `--gemini-model` | _(empty)_ | Specific Gemini model to use (e.g., `gemini-3-pro`) |
| `--debug`        | `false`   | Enable debug logging                                |

### API Endpoints

- **`POST /session/create`**: Create a new ACP session and spawn a Gemini CLI
  subprocess.
- **`POST /session/prompt`**: Send a prompt to an existing session (maintains
  context for multi-turn conversations).
- **`POST /session/close`**: Terminate the session and clean up the subprocess.
- **`GET /health`**: Health check endpoint.

_For detailed API payload structures and OpenClaw Agent integration guide,
please refer to the `SKILL.md` file._
