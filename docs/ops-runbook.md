# 運用 Runbook

conoha-proxy を本番で動かすオペレータ向けの手順集。すべての admin API 呼び出しは Unix socket (`/var/run/conoha-proxy.sock`) を前提とする。外部からは SSH トンネル越しに叩く。

API のフィールド定義は [admin-api.md](admin-api.md) を参照。

## 新しいサービスを立ち上げる

ドメインを初めて登録し、最初の upstream を載せる流れ。3 ステップで完了する。

### 1. サービスを登録する

```bash
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "myapp",
    "hosts": ["app.example.com"],
    "health_policy": {"path": "/up", "interval_ms": 5000}
  }'
```

成功すると `201` が返り、同時に証明書の発行も始まる (certmagic が裏で ACME HTTP-01 を回す)。登録直後に `GET /v1/services/myapp` を叩くと `active_target` が `null` で、この時点で該当ホストにアクセスすると **503** が返る (想定動作)。

### 2. upstream コンテナを起動する

本プロキシの責務外。`conoha-cli` などで `docker run app:v1` を実行し、ローカルの任意ポート (例: `127.0.0.1:9001`) で listen させる。

### 3. 初回デプロイ

```bash
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services/myapp/deploy \
  -H 'Content-Type: application/json' \
  -d '{"target_url": "http://127.0.0.1:9001"}'
```

conoha-proxy は同期的に `/up` を叩き、200 を確認できたら active に昇格させる。返り値 `200` が得られれば配信開始。

### 4. 動作確認

```bash
curl -I https://app.example.com/
# HTTP/2 200

curl --unix-socket /var/run/conoha-proxy.sock \
  http://admin/v1/services/myapp | jq .
# "active_target": { "url": "http://127.0.0.1:9001", ... }
```

証明書がまだ未発行の場合は `tls_status` が `pending` になる。反映まで数秒〜数十秒。

## blue/green スワップ

新バージョンを別ポートで立ち上げ、admin API でフリップする。

```bash
# 新バージョンの upstream を起動 (別ポート)
# docker run -d -p 127.0.0.1:9002:8080 myapp:v2

# swap + 30 秒の drain 窓
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services/myapp/deploy \
  -H 'Content-Type: application/json' \
  -d '{"target_url": "http://127.0.0.1:9002", "drain_ms": 30000}'

# 新 upstream に切り替わっているかを確認
curl -sI https://app.example.com/ | head -1
curl --unix-socket /var/run/conoha-proxy.sock \
  http://admin/v1/services/myapp | jq '.active_target.url, .draining_target.url'
# "http://127.0.0.1:9002"
# "http://127.0.0.1:9001"
```

30 秒後、`draining_target` は自動的に `null` になる。旧コンテナの `docker stop` は呼び出し側で別途実施する (本プロキシは関知しない)。

## ロールバック

**drain 窓の中でのみ有効**。新デプロイ直後に問題が発覚した場合に使う。

```bash
curl --unix-socket /var/run/conoha-proxy.sock \
  http://admin/v1/services/myapp/rollback \
  -X POST
```

active と draining を入れ替え、drain deadline を `now + 既定 drain` (30 秒) に再設定する。drain 窓が既に切れていた場合は **409 `no_drain_target`** が返る。この場合は旧バージョンを再度 `deploy` するしかない。

### drain 窓が切れているとき

```bash
# 新しいコンテナとして旧バージョンを再起動し、deploy し直す
# docker run -d -p 127.0.0.1:9003:8080 myapp:v1
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services/myapp/deploy \
  -H 'Content-Type: application/json' \
  -d '{"target_url": "http://127.0.0.1:9003"}'
```

## サービスのライフサイクル

外部観測可能な状態は 3 つ。

| Phase | 意味 |
|---|---|
| `configured` | サービス登録済みだが active target が未設定。該当ホストへの要求は 503 |
| `live` | active target が設定され、通常運転中 |
| `swapping` | draining target が存在し、drain 窓の内側。rollback 可能な期間 |

内部的には `deploy` 実行中に probing が走るが、API 呼び出しの中で同期完了するため、routing や Phase には一切現れない。

```
[ 未登録 ] ── upsert ──▶ [ configured ]
                              │ deploy (probe 成功)
                              ▼
                         [ live ] ────────┐
                           ▲              │ deploy (probe 成功)
                           │              ▼
                           └── drain 経過 ─ [ swapping ] ── rollback
```

## エラーレスポンス早見表

Admin API と proxy path でのエラー対応表。仕様書 §8 と整合。

| Category | 発生例 | HTTP | 対応 |
|---|---|---|---|
| Upstream 無応答 | コンテナクラッシュ | `502` | health worker が後追い検知。`/v1/services/{name}` で health 状況を確認 |
| Upstream 遅延 | response > WriteTimeout | `504` | WriteTimeout で cut される。upstream 側のタイムアウトを要調整 |
| 証明書発行失敗 | ACME rate limit, DNS 未伝播 | — | HTTPS は失敗、HTTP redirect も停止。`GET /v1/services/{name}` が `tls_status: pending` または `failed` を返す |
| Probe 失敗で deploy 拒否 | `/up` が 3 回連続で NG | `424` `probe_failed` | 新 target を一切採用しないまま終了。upstream のログを確認 |
| drain 中の新 deploy | draining 存在時に再 deploy | `200` | 新 probe 通過で `(new, 旧 active)` に回転、既存 draining は破棄 |
| Store 書込失敗 | ディスク満杯、bbolt error | `503` `store_error` | ディスク空き容量と `state.db` のパーミッションを確認 |
| Host mismatch | 未登録ホストへのアクセス | `421` | plain-text で `421 Misdirected Request`。登録漏れを確認 |
| パニック | コード不具合 | `500` | goroutine 内で recover される。JSON ログに `level=error` で出力。issue 報告対象 |
| サービス未存在 | 削除済み名で API 呼び出し | `404` `not_found` | GET / deploy / rollback / delete 全てに該当 |
| バリデーション失敗 | 空の name, 不正な target URL | `400` `validation_failed` | リクエスト body を確認 |
| Admin TCP bind 非 loopback | `--admin-tcp 0.0.0.0:9999` | — | 起動時に fatal。loopback (127.0.0.1 など) 以外は必ず拒否 |

## バックアップとリストア

状態ファイルは `/var/lib/conoha-proxy/state.db` の 1 ファイル。証明書は `/var/lib/conoha-proxy/certs/` のディレクトリツリー。

### スナップショットを取る

```bash
# コンテナを止めずに単純コピー (bbolt は MMAP、稼働中 cp は整合性に難)
# 推奨: 短時間止めてから取得
docker stop conoha-proxy
cp /var/lib/conoha-proxy/state.db /var/backups/conoha-proxy/state.db.$(date +%F)
tar czf /var/backups/conoha-proxy/certs.$(date +%F).tgz /var/lib/conoha-proxy/certs/
docker start conoha-proxy
```

Docker ボリュームを丸ごと取る場合:

```bash
docker run --rm \
  -v conoha-proxy-data:/data \
  -v "$(pwd)":/backup \
  alpine tar czf /backup/conoha-proxy-$(date +%F).tgz /data
```

**証明書も同時にバックアップすること**。state.db だけ復元すると、ドメイン一覧は戻っても証明書が失われるため再発行になり、ACME rate limit に引っかかる可能性がある。

### リストア

```bash
docker stop conoha-proxy
cp /var/backups/conoha-proxy/state.db.2026-04-20 /var/lib/conoha-proxy/state.db
tar xzf /var/backups/conoha-proxy/certs.2026-04-20.tgz -C /
docker start conoha-proxy
```

起動時に `store.LoadAll` で再構築され、以前の services とドメインがそのまま復元される (`cmd/conoha-proxy/main.go: runProxy`)。

## ログ解析

構造化 JSON で stdout に出る (`internal/logging/slog.go`)。`docker logs conoha-proxy | jq ...` で絞り込む。

```bash
# listener エラー
docker logs conoha-proxy | jq 'select(.msg == "listener error")'

# ACME 関連
docker logs conoha-proxy | jq 'select(.logger | test("certmagic"))'

# 特定サービスの状態変化
docker logs conoha-proxy | jq 'select(.service == "myapp")'

# 過去 1 時間のエラーだけ
docker logs --since 1h conoha-proxy | jq 'select(.level == "ERROR")'
```

主要キー:

- `time`, `level`, `msg` — 標準の slog フィールド
- `service` — service name (該当するイベントのみ)
- `target` — upstream URL
- `err` — エラーメッセージ

## アップグレード手順

1. 新しいイメージタグをプルする。
   ```bash
   docker pull ghcr.io/crowdy/conoha-proxy:v0.N+1
   ```
2. 現行コンテナを止める。
   ```bash
   docker stop conoha-proxy
   ```
   STOPSIGNAL SIGTERM + 最大 60 秒で graceful shutdown する。
3. 同じボリューム構成で新しいコンテナを起動する。
   ```bash
   docker rm conoha-proxy
   docker run -d --name conoha-proxy \
     -p 80:80 -p 443:443 \
     -v conoha-proxy-data:/var/lib/conoha-proxy \
     -v /var/run/conoha-proxy.sock:/var/run/conoha-proxy.sock \
     ghcr.io/crowdy/conoha-proxy:v0.N+1 \
     run --acme-email=admin@example.com
   ```
4. 起動直後に以下を確認する。
   ```bash
   curl --unix-socket /var/run/conoha-proxy.sock http://admin/healthz
   curl --unix-socket /var/run/conoha-proxy.sock http://admin/readyz
   curl --unix-socket /var/run/conoha-proxy.sock http://admin/version
   curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services | jq '.services | length'
   ```

### ロールバック手順

新バージョンで問題が出た場合:

1. コンテナを止める。
   ```bash
   docker stop conoha-proxy && docker rm conoha-proxy
   ```
2. 直前のスナップショットから `state.db` と `certs/` を戻す (前述のリストア手順)。
3. 旧イメージタグで再起動する。

スキーマバージョンが前進マイグレーションのみサポートの点に注意 (`meta/schema_version`)。マイナーアップデート内ではダウングレードは原則安全だが、メジャー更新時は事前にスナップショット取得が必須。
