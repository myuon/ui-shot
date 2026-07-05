# ui-shot MCP server (Cloudflare Workers)

R2 に保存された ui-shot のスクリーンショットを MCP (Model Context Protocol) 経由で
扱うための Cloudflare Workers サーバーです。AI エージェント (Claude Code など) から
スクリーンショットのアップロード・一覧取得・URL の組み立てができます。

Go CLI (`uishot`) とは完全に独立して動作します。CLI は不要で、同じ R2 バケット・
同じオブジェクトキー規約 (`<repo>/pr-<n>/<sha>/<name>.<ext>`) を共有するだけです。

## アーキテクチャ

- ステートレス Worker + [`createMcpHandler`](https://developers.cloudflare.com/agents/model-context-protocol/mcp-handler-api/) (`agents/mcp`)
- トランスポートは Streamable HTTP (エンドポイント: `/mcp`)
- Durable Object 不要 — 永続化は R2 自体が担う
- 認証は静的 Bearer トークン (Cloudflare secret `MCP_AUTH_TOKEN`、定数時間比較で検証)

## ツール

| ツール名 | 説明 | 主な引数 |
|---|---|---|
| `upload_screenshot` | base64 画像を R2 に保存し公開 URL を返す | `repo`, `pr`/`issue`, `commit`, `name`, `data` (base64), `content_type` (任意) |
| `list_screenshots` | repo と PR (または issue) に紐づくスクリーンショットの一覧 (key + URL の JSON) | `repo`, `pr`/`issue` |
| `get_screenshot_url` | キー構成要素から公開 URL を組み立てる (R2 アクセスなし) | `repo`, `pr`/`issue`, `commit`, `name`, `ext` |

- `pr` と `issue` は相互排他で、どちらか一方が必須です。
- `upload_screenshot` は PNG / JPEG / WebP / GIF に対応し、デコード後 10 MiB を上限とします。
  画像形式はマジックバイトから自動判定します (判定できない場合は `content_type` ヒントを使用)。
- オブジェクトキーは CLI の `internal/provider/provider.go` の `ObjectKey()` と同一規約:

```
PR:    <repo>/pr-<number>/<commit>/<name>.<ext>
Issue: <repo>/issue-<number>/<commit>/<name>.<ext>
```

アップロードされたオブジェクトには `Cache-Control: public, max-age=31536000, immutable`
と拡張子由来の `Content-Type` が設定されます (CLI と同じ挙動)。

## 前提条件

- Node.js 20+
- Cloudflare アカウント (R2 有効化済み)
- [wrangler](https://developers.cloudflare.com/workers/wrangler/) (devDependencies に含まれるため `npx wrangler` で利用可)
- R2 バケット — `uishot setup --provider r2` で作成したもの (デフォルト名: `ui-shot-assets`)、
  または `wrangler r2 bucket create <bucket>` で作成したもの
- バケットの**公開 base URL** — バケット名からは導出できないため、以下のいずれかで用意:
  - r2.dev マネージドドメインを有効化 (開発用途、レート制限あり):
    `npx wrangler r2 bucket dev-url enable <bucket>` が `https://pub-<hash>.r2.dev` を表示
  - Cloudflare ダッシュボードでバケットにカスタムドメインを割り当て (本番推奨)

## セットアップとデプロイ

```bash
cd workers/mcp-server
npm install
```

### 1. wrangler.jsonc を編集

- `r2_buckets[0].bucket_name` — 使用するバケット名に変更 (CLI のデフォルトは `ui-shot-assets`)
- `vars.PUBLIC_BASE_URL` — バケットの公開 URL (r2.dev ドメインまたはカスタムドメイン) に変更

### 2. 認証トークンを設定

```bash
# トークンを生成 (例)
openssl rand -hex 32

# secret として登録 (プロンプトに生成したトークンを貼り付け)
npx wrangler secret put MCP_AUTH_TOKEN
```

secret が未設定の場合、Worker は全リクエストに 500 を返します (fail-closed)。

### 3. デプロイ

```bash
npx wrangler deploy
```

`https://ui-shot-mcp.<account>.workers.dev/mcp` が MCP エンドポイントになります。

動作確認:

```bash
curl -s https://ui-shot-mcp.<account>.workers.dev/mcp \
  -X POST \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"curl","version":"0.0.0"}}}'
```

トークンなしのリクエストは 401 を返します。

## MCP クライアント設定

### Claude Code

```bash
claude mcp add --transport http ui-shot \
  https://ui-shot-mcp.<account>.workers.dev/mcp \
  --header "Authorization: Bearer <token>"
```

### 汎用 JSON 設定 (Claude Desktop / Cursor など)

```json
{
  "mcpServers": {
    "ui-shot": {
      "type": "http",
      "url": "https://ui-shot-mcp.<account>.workers.dev/mcp",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

## 開発

```bash
npm run typecheck   # tsc --noEmit
npm test            # vitest (純粋関数のユニットテスト)
npm run dev         # wrangler dev (ローカル起動)
```

ローカル開発時の secret は `.dev.vars` ファイルで渡せます (git 管理外):

```
MCP_AUTH_TOKEN=dev-token
```

## CLI との関係

- CLI (`uishot upload`) と `upload_screenshot` は同じバケット・同じキー規約で動作するため相互互換です。
- CLI の設定 (`~/.config/uishot/config.toml`) とは独立しており、Worker は
  `wrangler.jsonc` のバインディングで直接バケットを参照します。
