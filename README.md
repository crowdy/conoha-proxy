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

```bash
docker run -d --name conoha-proxy \
  -p 80:80 -p 443:443 \
  -v conoha-proxy-data:/var/lib/conoha-proxy \
  -v /var/run/conoha-proxy.sock:/var/run/conoha-proxy.sock \
  ghcr.io/crowdy/conoha-proxy:latest \
  run --acme-email=admin@example.com

# サービス登録
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services \
  -d '{"name":"myapp","hosts":["app.example.com"]}'

# 初回デプロイ
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services/myapp/deploy \
  -d '{"target_url":"http://127.0.0.1:9001"}'
```

## ドキュメント

- [docs/architecture.md](docs/architecture.md) — 内部アーキテクチャとコンポーネント構成
- [docs/ops-runbook.md](docs/ops-runbook.md) — 運用手順 (デプロイ、ロールバック、バックアップ)
- [docs/admin-api.md](docs/admin-api.md) — Admin HTTP API リファレンス

## ライセンス

Apache-2.0 — [LICENSE](LICENSE)。サードパーティライブラリは [NOTICES.md](NOTICES.md) を参照。
