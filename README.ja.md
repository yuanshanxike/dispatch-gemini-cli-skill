# Gemini-CLI ACP Bridge for Claw

[English](./README.md) | [中文](./README.zh.md)

---

## はじめに

[ACP (Agent Client Protocol)](https://github.com/coder/acp-go-sdk) を介して
Gemini CLI とのマルチターン会話のトンネリングを実装するブリッジです。

このプロジェクトは HTTP サーバーを提供し、OpenClaw エージェントが Gemini CLI
とのマルチターン会話セッションをシームレスに作成、使用、および管理できるようにします。

### 前提条件

1. **Gemini CLI**: インストールされており、システムの `PATH`
   で利用可能である必要があります。
   ```bash
   npm install -g @google/gemini-cli
   ```
2. **Go 環境**（_任意_）: Go 1.21 以上（自分でプロジェクトをビルドしたい場合）

### クイックスタート

1. _Releases ページ_
   にアクセスし、お使いのシステムに適したパッケージ（macOS/Linux の場合は
   `.tar.gz`、Windows の場合は `.zip`）をダウンロードします。
2. アーカイブを展開します。展開されたディレクトリには `acp-bridge` バイナリと
   `SKILL.md` ファイルが含まれています。
3. エージェントにこの skill を使用させます（サーバーのデフォルトポート 9090）。

### ソースからのビルド（任意）

自分でプロジェクトをビルドしたい場合：

```bash
# クローンしてディレクトリに入る
git clone git@github.com:yuanshanxike/dispatch-gemini-cli-skill.git

# プロジェクトをビルドする
go build -o acp-bridge .
```

### コマンドライン引数

| フラグ           | デフォルト値 | 説明                                               |
| ---------------- | ------------ | -------------------------------------------------- |
| `--port`         | `9090`       | HTTP サーバーのポート                              |
| `--gemini-bin`   | `gemini`     | Gemini CLI バイナリのパス                          |
| `--gemini-model` | _(空)_       | 使用する特定の Gemini モデル（例：`gemini-3-pro`） |
| `--debug`        | `false`      | 詳細なデバッグログを有効にする                     |

### API エンドポイント

- **`POST /session/create`**: 新しい ACP セッションを作成し、Gemini CLI
  サブプロセスを起動します。
- **`POST /session/prompt`**:
  既存のセッションにプロンプトを送信します（マルチターン会話のコンテキストを保持します）。
- **`POST /session/close`**:
  セッションを終了し、サブプロセスをクリーンアップします。
- **`GET /health`**: ヘルスチェック用エンドポイント。

_API の詳細なペイロード構造や OpenClaw
エージェントの統合ガイドについては、`SKILL.md` ファイルを参照してください。_
