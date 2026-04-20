# conoha-proxy

[![ci](https://github.com/crowdy/conoha-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/crowdy/conoha-proxy/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

ConoHa VPS 向けの Go 製リバースプロキシデーモン。自動 HTTPS (Let's Encrypt)、マルチサービスのドメイン別ルーティング、blue/green デプロイを提供する。

[English](README-en.md) | [한국어](README-ko.md)

## 特徴

- 単一 Go バイナリ、Docker イメージとして配布
- Let's Encrypt 自動発行・更新 (HTTP-01 challenge)
- サービス単位の blue/green ターゲットスワップ + drain
- ヘルスチェック (HTTP) に基づくデプロイ合否判定
- Admin HTTP API (Unix socket または localhost TCP)
- 構造化 JSON ログ
- Apache-2.0

## 配置

```
[Internet]
    │ :80, :443
    ▼
┌─────────────────────────────┐
│ ConoHa VPS                  │
│ ┌─────────────────────────┐ │
│ │ conoha-proxy (Docker)   │ │
│ │ - /var/lib/conoha-proxy │◀── host volume (state + certs)
│ │ - host network / -p     │ │
│ └────────┬────────────────┘ │
│          │ upstream         │
│          ▼                  │
│ ┌─────────────────────────┐ │
│ │ app1 container (green)  │ │
│ │ app1 container (blue)   │ │
│ │ app2 container ...      │ │
│ └─────────────────────────┘ │
│          ▲                  │
│          │ SSH              │
└──────────┼──────────────────┘
           │
     [conoha-cli (local)]
      └─ SSH → docker run app:new
      └─ SSH → curl --unix-socket ... /deploy
```

## クイックスタート

Admin Unix socket は **data volume の中** (`/var/lib/conoha-proxy/admin.sock`) に配置される。distroless の `nonroot` ユーザーが確実に書き込める場所はそこだけのため。ホストから操作するときは data volume を bind-mount する。

```bash
# ホスト側でディレクトリを用意し、コンテナ内 nonroot (uid 65532) に所有権を渡す
sudo mkdir -p /var/lib/conoha-proxy
sudo chown 65532:65532 /var/lib/conoha-proxy

docker run -d --name conoha-proxy \
  -p 80:80 -p 443:443 \
  -v /var/lib/conoha-proxy:/var/lib/conoha-proxy \
  ghcr.io/crowdy/conoha-proxy:latest \
  run --acme-email=admin@example.com

# サービス登録 (ホスト側から同じパスでアクセス)
curl --unix-socket /var/lib/conoha-proxy/admin.sock http://admin/v1/services \
  -d '{"name":"myapp","hosts":["app.example.com"]}'

# 初回デプロイ
curl --unix-socket /var/lib/conoha-proxy/admin.sock http://admin/v1/services/myapp/deploy \
  -d '{"target_url":"http://127.0.0.1:9001"}'
```

## ドキュメント

- [docs/architecture.md](docs/architecture.md) — 内部アーキテクチャとコンポーネント構成
- [docs/ops-runbook.md](docs/ops-runbook.md) — 運用手順 (デプロイ、ロールバック、バックアップ)
- [docs/admin-api.md](docs/admin-api.md) — Admin HTTP API リファレンス

## ライセンス

Apache-2.0 — [LICENSE](LICENSE)。サードパーティライブラリは [NOTICES.md](NOTICES.md) を参照。
