# Admin API リファレンス

conoha-proxy の Admin HTTP API は `/v1/` をプレフィックスとし、それ以外に `/healthz`, `/readyz`, `/version` を持つ。外部公開は想定せず、Unix socket または loopback TCP 経由でのみアクセスする。

- 既定の Unix socket: `/var/run/conoha-proxy.sock` (mode `0660`)
- オプションの TCP: `--admin-tcp 127.0.0.1:9999` (非 loopback は起動時に fatal)
- 認証機構は MVP では持たない (FS 権限 + loopback で代替)

全てのレスポンスは `application/json; charset=utf-8`。エラー時の body は次の形式に統一:

```json
{"error": {"code": "...", "message": "..."}}
```

---

## エンドポイント一覧

| Method | Path | 概要 |
|---|---|---|
| `POST` | `/v1/services` | サービスの upsert |
| `GET` | `/v1/services` | サービス一覧 |
| `GET` | `/v1/services/{name}` | サービス詳細 |
| `POST` | `/v1/services/{name}/deploy` | 新ターゲットへの probe + swap |
| `POST` | `/v1/services/{name}/rollback` | drain 窓内での即時ロールバック |
| `DELETE` | `/v1/services/{name}` | サービス削除 |
| `GET` | `/healthz` | プロキシ自身の liveness |
| `GET` | `/readyz` | store 読み込み済みかどうか |
| `GET` | `/version` | ビルド版情報 |

---

## `POST /v1/services`

サービスの upsert。name が既存なら `hosts` と `health_policy` を差し替える (active/draining/deadline は保持)。

### Request body

| Field | 型 | 必須 | 説明 |
|---|---|---|---|
| `name` | string | yes | サービス識別子。ドメインとは別の論理名 |
| `hosts` | string[] | yes | マッチする Host ヘッダー (複数可、大文字小文字無視) |
| `health_policy` | object | no | ヘルスチェック設定 (省略時は既定値) |
| `health_policy.path` | string | no | probe パス (既定 `/up`) |
| `health_policy.interval_ms` | int | no | 周期 (既定 `5000`) |
| `health_policy.timeout_ms` | int | no | probe タイムアウト (既定 `2000`) |
| `health_policy.healthy_threshold` | int | no | 連続成功閾値 (既定 `1`) |
| `health_policy.unhealthy_threshold` | int | no | 連続失敗閾値 (既定 `3`) |

### Example

```bash
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "myapp",
    "hosts": ["app.example.com"],
    "health_policy": {"path": "/up", "interval_ms": 5000}
  }'
```

### Response

- `201 Created` — 成功。body は保存後の Service オブジェクト
- `400 Bad Request` — `invalid_body` (JSON パース失敗) または `validation_failed` (name 空、hosts 重複など)
- `503 Service Unavailable` — `store_error` / `tls_error` / `reload_failed`

```json
{
  "name": "myapp",
  "hosts": ["app.example.com"],
  "active_target": null,
  "draining_target": null,
  "health_policy": {
    "path": "/up", "interval_ms": 5000, "timeout_ms": 2000,
    "healthy_threshold": 1, "unhealthy_threshold": 3
  },
  "created_at": "2026-04-20T09:00:00Z",
  "updated_at": "2026-04-20T09:00:00Z"
}
```

---

## `GET /v1/services`

登録済みサービスの一覧。

### Example

```bash
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services
```

### Response

- `200 OK`
- `503` — `store_error`

```json
{
  "services": [
    { "name": "myapp", "hosts": ["app.example.com"], "...": "..." },
    { "name": "admin", "hosts": ["admin.example.com"], "...": "..." }
  ]
}
```

---

## `GET /v1/services/{name}`

サービス単体の詳細。

### Example

```bash
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services/myapp
```

### Response

- `200 OK` — Service オブジェクト (`POST /v1/services` のレスポンスと同形式)
- `404 Not Found` — `not_found`
- `503` — `store_error`

---

## `POST /v1/services/{name}/deploy`

新ターゲットを probe し、成功した場合に blue/green をスワップする。probe は admin API の呼び出しスレッドで同期的に実行される。

### Request body

| Field | 型 | 必須 | 説明 |
|---|---|---|---|
| `target_url` | string | yes | 新しい upstream URL。`http://` または `https://` で始まる必要がある |
| `drain_ms` | int | no | 既存 active を draining に降格する持続時間 (ms)。既定 `30000` |

### Example

```bash
curl --unix-socket /var/run/conoha-proxy.sock \
  http://admin/v1/services/myapp/deploy \
  -H 'Content-Type: application/json' \
  -d '{"target_url": "http://127.0.0.1:9001", "drain_ms": 30000}'
```

### Response

- `200 OK` — 更新後の Service オブジェクト。`active_target` が新 URL、既存があれば `draining_target` に回る
- `400` — `invalid_body` / `validation_failed` (target URL のスキームまたは host が不正)
- `404` — `not_found`
- `424 Failed Dependency` — `probe_failed`。probe が連続失敗した場合。state は一切変更されない
- `503` — `store_error` / `reload_failed`

---

## `POST /v1/services/{name}/rollback`

直前の deploy を drain 窓内で巻き戻す。active と draining を入れ替え、drain deadline をリセットする。

### Example

```bash
curl --unix-socket /var/run/conoha-proxy.sock \
  http://admin/v1/services/myapp/rollback \
  -X POST
```

### Response

- `200 OK` — 更新後の Service オブジェクト
- `404` — `not_found`
- `409 Conflict` — `no_drain_target` (drain 窓が既に閉じている)
- `503` — `store_error` / `reload_failed`

---

## `DELETE /v1/services/{name}`

サービスを削除する。以後、該当 hosts への外部リクエストは `421 Misdirected Request`。

### Example

```bash
curl --unix-socket /var/run/conoha-proxy.sock \
  http://admin/v1/services/myapp \
  -X DELETE
```

### Response

- `204 No Content`
- `503` — `store_error` / `reload_failed`

---

## `GET /healthz`

プロキシプロセス自身の liveness (常に 200)。

```bash
curl --unix-socket /var/run/conoha-proxy.sock http://admin/healthz
# {"status":"ok"}
```

---

## `GET /readyz`

store 読み込みが完了し、リクエストを処理できる状態かどうか。

```bash
curl --unix-socket /var/run/conoha-proxy.sock http://admin/readyz
# {"status":"ok"}
```

---

## `GET /version`

ビルド時に `-ldflags` で注入されたバージョン文字列を返す。

```bash
curl --unix-socket /var/run/conoha-proxy.sock http://admin/version
# {"version":"v0.1.0"}
```

---

## エラーコード一覧

| Code | 主な HTTP | 意味 / 典型的原因 |
|---|---|---|
| `not_found` | `404` | 該当 service 名が存在しない / ルートが存在しない |
| `validation_failed` | `400` | name 空、hosts 重複、target URL スキーム不正 |
| `probe_failed` | `424` | 新ターゲットの health probe が連続失敗 |
| `store_error` | `503` | bbolt 書込/読取失敗 (ディスク満杯など) |
| `tls_error` | `503` | certmagic `ManageDomains` が失敗 |
| `reload_failed` | `503` | snapshot 再構築または router reload 失敗 |
| `no_drain_target` | `409` | rollback 対象の draining target が存在しない |
| `method_not_allowed` | `405` | 既知のパスに対して許可されていないメソッド |
| `invalid_body` | `400` | JSON パース失敗 |

---

## curl cheat sheet (happy path)

```bash
SOCK=/var/run/conoha-proxy.sock
BASE="http://admin"

# 登録
curl --unix-socket $SOCK $BASE/v1/services \
  -d '{"name":"myapp","hosts":["app.example.com"]}'

# 一覧
curl --unix-socket $SOCK $BASE/v1/services

# 詳細
curl --unix-socket $SOCK $BASE/v1/services/myapp

# 初回デプロイ
curl --unix-socket $SOCK $BASE/v1/services/myapp/deploy \
  -d '{"target_url":"http://127.0.0.1:9001"}'

# blue/green
curl --unix-socket $SOCK $BASE/v1/services/myapp/deploy \
  -d '{"target_url":"http://127.0.0.1:9002","drain_ms":30000}'

# ロールバック (drain 窓内のみ)
curl --unix-socket $SOCK $BASE/v1/services/myapp/rollback -X POST

# 削除
curl --unix-socket $SOCK $BASE/v1/services/myapp -X DELETE

# liveness / readiness / version
curl --unix-socket $SOCK $BASE/healthz
curl --unix-socket $SOCK $BASE/readyz
curl --unix-socket $SOCK $BASE/version
```
