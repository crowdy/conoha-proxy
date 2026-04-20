# conoha-proxy — Design Spec

- 作成日: 2026-04-20
- ステータス: ドラフト (MVP 対象)
- Module: `github.com/crowdy/conoha-proxy`
- License: Apache-2.0
- Go: 1.22+

---

## 1. 目的とスコープ

ConoHa VPS 上の Docker コンテナ群の前段に立ち、**自動 HTTPS + マルチサービスのドメイン別ルーティング + blue/green デプロイ**を担うリバースプロキシデーモン。kamal-proxy のコンセプトを参考にするが、API・コードベースは完全に独立した Go 実装。

### スコープに含む (MVP)

- マルチサービス、Host ヘッダー一致によるルーティング
- サービス単位の blue/green ターゲット切り替え + drain
- Let's Encrypt 自動 HTTPS (HTTP-01 のみ)
- ヘルスチェックと probe ベースのデプロイ合否
- Admin HTTP API (Unix socket + 127.0.0.1 TCP バインド限定)
- bbolt による状態永続化
- 構造化 JSON ログ (stdout)
- Docker イメージ配布 (主) + Go バイナリ (副)

### スコープに含まない (明示的に除外)

- コンテナのライフサイクル管理 (起動・停止・イメージ pull 等) — 呼び出し側の責務
- DNS / 証明書 DNS-01 チャレンジ — 将来の拡張余地のみ確保
- 外部ネットワーク越しの Admin API (SSH トンネル前提)
- Basic Auth / IP allowlist / rate limit / メンテナンスモード — 将来
- Prometheus メトリクス / OpenTelemetry トレース — 将来
- 分散 / マルチインスタンス — 単一プロセス前提
- パスベースルーティング / ワイルドカードホスト — 将来 (DNS-01 対応と同時)

### 成功条件 (MVP Done Definition)

以下 6 シナリオの e2e テストが緑、かつ Let's Encrypt staging で 1 ドメインの証明書発行を手動で成功させること。

1. サービス登録 → 503 (active_target 未設定)
2. 初回デプロイ → 200 (upstream 応答)
3. blue/green スワップ → 200 (新しい upstream 応答)
4. drain ウィンドウ内 rollback → 200 (旧 upstream 応答に復帰)
5. probe 失敗時のデプロイ拒否 → 424 + 状態変化なし
6. サービス削除 → 以後 421

---

## 2. 役割分担

| 主体 | 担当 | 担当外 |
|---|---|---|
| **conoha-proxy** (本プロジェクト) | HTTP(S) 終端、自動 TLS、ドメインルーティング、blue/green ターゲットスワップ、ヘルスチェック | コンテナ起動/停止、イメージビルド/push、DNS |
| conoha-cli (別プロジェクト) | コンテナライフサイクル、SSH、デプロイオーケストレーション、Admin API 呼び出し | プロキシ / TLS |
| OS / Docker | コンテナランタイム | — |

**中核原則**: conoha-proxy は "ルーティングの真実の保管庫兼 TLS 終端" に徹する。呼び出し側が `POST /deploy` で指定するまでは、旧ターゲットへ流し続ける。

---

## 3. アーキテクチャ

### 3.1 プロセスモデル

```
conoha-proxy (single Go binary)
├─ HTTP  :80  (ACME http-01 challenge + HTTPS redirect)
├─ HTTPS :443 (TLS termination + reverse proxy)
├─ Admin  /var/run/conoha-proxy.sock (Unix) または 127.0.0.1:9999 (TCP)
└─ Background:
   ├─ certmagic manager (renewal)
   ├─ Health worker (per active/draining target)
   └─ Persistence flusher (bbolt commit on state change)
```

### 3.2 デプロイ配置図 (README にも掲載予定)

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

---

## 4. コンポーネント

一方向依存・単一責務・独立テスト可能性を優先する。

| # | Component | Responsibility | Depends on |
|---|---|---|---|
| C1 | `internal/router` | Request → service/active target lookup, `httputil.ReverseProxy` wrap | C2 (read) |
| C2 | `internal/store` | Persistent KV (bbolt), transactions, snapshots | `go.etcd.io/bbolt` |
| C3 | `internal/service` | Domain model: `Service`, `Target`, `HealthPolicy` + state machine | C2 |
| C4 | `internal/health` | Per-target health worker + one-shot probe | C1, C3 |
| C5 | `internal/tls` | certmagic wrapper: issuance/renewal/storage | `certmagic` |
| C6 | `internal/adminapi` | Admin HTTP API (Unix/TCP listener) | C3 |
| C7 | `internal/logging` | Structured JSON logger (`log/slog`) | stdlib |
| C8 | `cmd/conoha-proxy` | Main binary + cobra subcommands | C1〜C7 |

### 4.1 主要インターフェース

```go
// C1
type Router interface {
    ServeHTTP(http.ResponseWriter, *http.Request)
    Reload(snapshot ServiceSnapshot) error  // 原子的置き換え
}

// C2
type Store interface {
    LoadAll(ctx context.Context) ([]Service, error)
    SaveService(ctx context.Context, svc Service) error
    DeleteService(ctx context.Context, name string) error
    Tx(fn func(Tx) error) error
}

// C3
type Service struct {
    Name            string          // "myapp" (ホストと分離した識別子)
    Hosts           []string        // ["a.com", "www.a.com"]
    ActiveTarget    *Target
    DrainingTarget  *Target
    HealthPolicy    HealthPolicy
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
type Target struct {
    URL         string     // "http://127.0.0.1:9001"
    DeployedAt  time.Time
}

// C4
type Checker interface {
    Watch(t Target, p HealthPolicy) (<-chan HealthEvent, func())
    ProbeOnce(ctx context.Context, t Target, p HealthPolicy) error
}

// C5
type CertManager interface {
    GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error)
    ManageDomains(domains []string) error  // 動的追加/削除
}
```

### 4.2 依存方向

```
cmd ─▶ adminapi ─▶ service ─▶ store
          │           ▲
          ├──────────▶ router ◀──────── tls
          │              ▲
          └─────────────▶ health ───────▶ router (status transitions)
```

すべての I/O はインターフェース経由。テストで mock 差し替え可能。

### 4.3 明示的に除外するもの (YAGNI)

- ミドルウェア chain フレームワーク (Chi 等) — stdlib `http.Handler` で十分
- ORM — bbolt は KV、JSON シリアライズで足りる
- DI コンテナ — `main` で手動組み立て
- gRPC Admin API — HTTP+JSON がシンプルでデバッグ容易
- 分散・複数インスタンス — 単一プロセス前提

---

## 5. データモデル

### 5.1 bbolt bucket 構成

```
/var/lib/conoha-proxy/state.db
├─ services/         (bucket)
│   └─ <service-name> → JSON-encoded Service
└─ meta/             (bucket)
    ├─ schema_version → "1"
    └─ instance_id    → UUID
```

certmagic の証明書ストアは別ディレクトリ: `/var/lib/conoha-proxy/certs/` (certmagic デフォルト構造のまま)。

### 5.2 Service JSON スキーマ

```json
{
  "name": "myapp",
  "hosts": ["a.example.com", "www.a.example.com"],
  "active_target": {
    "url": "http://127.0.0.1:9001",
    "deployed_at": "2026-04-20T10:30:00Z"
  },
  "draining_target": {
    "url": "http://127.0.0.1:9002",
    "deployed_at": "2026-04-20T10:15:00Z"
  },
  "health_policy": {
    "path": "/up",
    "interval_ms": 5000,
    "timeout_ms": 2000,
    "healthy_threshold": 1,
    "unhealthy_threshold": 3
  },
  "created_at": "2026-04-20T09:00:00Z",
  "updated_at": "2026-04-20T10:30:00Z"
}
```

### 5.3 状態機械 (lifecycle)

```
  [ 未登録 ]
     │ upsert
     ▼
  [ Configured ]   active=nil, draining=nil
     │ deploy {new_target}
     ▼
  [ Probing ]      active=nil, probe=new
     │ probe 成功             │ probe 失敗
     ▼                         │
  [ Live ]                    └→ Configured 復帰, new 破棄
     │ deploy {new_target}
     ▼
  [ SwapProbing ] active=old, probe=new
     │ 成功                   │ 失敗
     ▼                         │
  [ Swapping ]                └→ active=old 維持, new 破棄
     │ active=new, draining=old, deadline=now+drain_ms
     │ drain 完了
     ▼
  [ Live ]         active=new, draining=nil
```

### 5.4 不変式

1. `active_target` が nil の間は、該当ホストへの要求は **503**
2. 新しいターゲットは probe を通過するまで**決して**トラフィックを受けない
3. スワップは原子的 (store トランザクション + router snapshot reload が 1 セット)
4. **Probing と SwapProbing は観測 API (Section 6, 8) では単一の `probing` フェーズに集約**する。内部ステートマシンとしての区別は実装詳細
5. **`drain_ms` は deploy 毎のパラメータ**であり、Service 自体には保持しない。次回 deploy で上書き指定が可能
6. Host ヘッダーの比較は**完全一致 (大文字小文字無視、ポート除外)**。ワイルドカードは MVP では非対応

### 5.5 マイグレーション戦略

- `meta/schema_version` でバージョン固定。現在は `"1"`
- 以後のスキーマ変更は起動時に `internal/migrations` で前進マイグレーション
- ダウングレードは非サポート

### 5.6 バックアップ・運用

- 単一ファイルなので `cp state.db state.db.bak` で即時スナップショット
- 証明書ディレクトリも同時バックアップ (ACME rate limit 回避)
- Docker ボリュームバックアップは `docker run --rm -v conoha-proxy-data:/data alpine tar czf /backup.tgz /data`

---

## 6. Admin API

プレフィックス: `/v1/`

| Method | Path | 動作 |
|---|---|---|
| POST   | `/v1/services` | Upsert (name を body に含む) |
| GET    | `/v1/services` | 一覧 |
| GET    | `/v1/services/{name}` | 詳細 (状態・TLS 状況・health 含む) |
| POST   | `/v1/services/{name}/deploy` | 新ターゲット指定, probe → swap |
| POST   | `/v1/services/{name}/rollback` | drain 窓内のみ有効 |
| DELETE | `/v1/services/{name}` | 削除 |
| GET    | `/healthz` | プロキシ自身の liveness |
| GET    | `/readyz` | store 読み込み済みかどうか |
| GET    | `/version` | バージョン情報 |

### 6.1 Request bodies (抜粋)

```json
// POST /v1/services
{
  "name": "myapp",
  "hosts": ["a.example.com"],
  "health_policy": { "path": "/up", "interval_ms": 5000 }
}

// POST /v1/services/{name}/deploy
{
  "target_url": "http://127.0.0.1:9001",
  "drain_ms": 30000
}
```

### 6.2 Admin API セキュリティ

- 既定: `/var/run/conoha-proxy.sock` (Unix socket, mode 0660, group 指定可)
- オプションで `127.0.0.1:9999` TCP バインド (localhost only — `0.0.0.0` バインドは**起動時に拒否**)
- 外部アクセスは SSH トンネル前提
- 認証機構は MVP では持たない (ソケットの FS 権限 + localhost バインドで代替)
- 起動時に TCP bind が localhost 以外を指していた場合、プロセスは致命エラーで終了

### 6.3 Responses and error contract

- 成功: `2xx` + JSON body
- クライアントエラー: `4xx` + `{"error": {"code": "...", "message": "..."}}`
- サーバーエラー: `5xx` + 同形式
- `POST /deploy` の probe 失敗は **424 Failed Dependency**, エラーコード `probe_failed`
- サービス未存在: **404** `not_found`
- Host mismatch (通常プロキシ経路): **421** (Admin API には該当しない)

---

## 7. データフロー

### 7.1 サービス登録

```
conoha-cli                    conoha-proxy
    │ POST /v1/services            │
    │ {name, hosts, health_policy} │
    ├─────────────────────────────▶│
    │                              │ store.SaveService
    │                              │ tls.ManageDomains(hosts)
    │                              │   └─ certmagic: ACME HTTP-01 発行
    │                              │ router.Reload(snapshot)
    │◀─────────────────────────────┤ 201
```

### 7.2 初回デプロイ (Configured → Live)

```
conoha-cli          conoha-proxy         new container
    │ 1. docker run ──────────────────────▶│ ready
    │                     │                │
    │ 2. POST /deploy     │                │
    │    {target_url, drain_ms}            │
    ├────────────────────▶│                │
    │                     │ health.ProbeOnce───▶│
    │                     │◀──── 200 ───────────│
    │                     │ tx {
    │                     │   svc.active = new
    │                     │   store.Save(svc)
    │                     │ }
    │                     │ router.Reload
    │◀────────────────────┤ 200
    │                     │
    │                     │ health.Watch(new) 開始 (周期)
```

### 7.3 blue/green スワップ

```
T0: active=old, draining=nil → 外部トラフィックは old へ
T1: conoha-cli が new (:9002) を起動
T2: POST /deploy {target_url: :9002, drain_ms: 30000}
    conoha-proxy:
      health.ProbeOnce(new) → success
      tx {
        svc.draining = old
        svc.active   = new
        svc.drain_deadline = T2 + drain_ms
        store.Save
      }
      router.Reload
    応答: 200

T2+ε: active=new, draining=old
      新規リクエスト → new
      in-flight リクエストは old が完走

T2 + drain_ms:
      deadline 到達 → router が draining ポインタを drop
      tx { svc.draining = nil }
      (旧コンテナ自体の docker stop は conoha-cli 側責務)
```

### 7.4 ロールバック

```
POST /v1/services/myapp/rollback
条件: svc.draining != nil (drain deadline 到達前)
動作:
  tx {
    (active, draining) = (draining, active)
    drain_deadline = now + drain_ms
    store.Save
  }
  router.Reload
```

drain 窓を逃した場合、旧ターゲットで再 deploy するしか手段がない (設計上の意図)。

### 7.5 証明書フロー

certmagic がバックグラウンドで以下を実行:

- 満了の 60〜90% 手前で自動更新
- 成功/失敗を logger へ出力
- 失敗時は ACME rate limit を考慮して再試行
- `certs/` ディレクトリへ永続化

`ManageDomains(hosts)` 呼び出し時:

- 新規ドメインは即時発行試行
- 削除ドメインは更新停止 (ファイル自体は保持、手動削除)

### 7.6 graceful shutdown

```
SIGTERM
  ├─ HTTP/HTTPS/Admin リスナーの accept を停止
  ├─ in-flight 要求を ReadHeaderTimeout + WriteTimeout 内で完走
  ├─ health worker 停止
  ├─ store 最終 flush
  └─ exit 0
```

Docker `STOPSIGNAL SIGTERM` + `--stop-timeout 60` と整合。

---

## 8. エラー処理 & エッジケース (ops-runbook.md にも反映予定)

| Category | Example | 対応 |
|---|---|---|
| Upstream 無応答 | コンテナクラッシュ | **502** + JSON log `kind=upstream_error`. health worker が別途検知 |
| Upstream 遅延 | response > timeout | WriteTimeout で cut, **504** |
| 証明書発行失敗 | ACME rate limit, DNS 未伝播 | HTTPS 不可、HTTP リダイレクトも停止。`GET /services/{name}` が `tls_status: pending_issuance` 返却 |
| Probe 失敗中 deploy | ProbeOnce 3 回連続失敗 | **424 Failed Dependency** + トランザクション roll back |
| drain 中の新 deploy | draining 存在時に新 deploy | new を probe → 通過で (new, previous active) に回転、既 draining は破棄 (drain 中要求のみ drain_ms 内維持) |
| store 書込失敗 | 容量満杯、bbolt error | **503** + in-memory 状態 rollback |
| Host mismatch | 未登録ホスト | **421 Misdirected Request** + plain-text |
| Panic | bug | `recover` で goroutine 内部捕捉, **500** + JSON log |

### 8.1 観測可能な状態

```json
// GET /v1/services/{name}
{
  "name": "myapp",
  "phase": "live | configured | probing | swapping",
  "active_target": { "...": "..." },
  "draining_target": { "...": "..." },
  "tls_status": "issued | pending | failed",
  "tls_error": "rate limit exceeded, retry after 2026-04-20T14:00Z",
  "last_deploy_at": "...",
  "health": {
    "active_consecutive_success": 4212,
    "active_last_checked_at": "..."
  }
}
```

---

## 9. テスト戦略

| Layer | Scope | Tooling |
|---|---|---|
| Unit | 純粋ロジック (state machine, ルーティング規則, snapshot reload) | `testing` stdlib + `testify/assert` |
| Integration | store ↔ service ↔ router 組み合わせ (実 bbolt ファイル、tmpfs) | `testing` + `t.TempDir` |
| E2E | 実 HTTPS サーバー起動、fake upstream、deploy → トラフィックスワップ検証 | `net/http/httptest` 製 upstream + (任意) testcontainers-go |

### 9.1 ACME テスト

- 実 Let's Encrypt への接続は禁止 (rate limit 回避)
- [Pebble](https://github.com/letsencrypt/pebble) を test harness に起動
- certmagic の `CA` を Pebble URL に差し替える test-mode ビルドタグ

### 9.2 Blue/Green シナリオ (代表例)

```go
func TestDeploy_BlueGreenSwap(t *testing.T) {
    h := setupTestHarness(t)
    h.AdminPOST("/v1/services", Service{Name: "myapp", Hosts: []string{"app.test"},
        HealthPolicy: HealthPolicy{Path: "/up"}})
    blue := h.SpawnUpstream(9001, returns200)
    green := h.SpawnUpstream(9002, returns200)

    h.AdminPOST("/v1/services/myapp/deploy", Deploy{URL: blue.URL})
    assertRequest(t, h.Through("app.test"), "blue-response")

    h.AdminPOST("/v1/services/myapp/deploy", Deploy{URL: green.URL, DrainMs: 100})
    assertRequest(t, h.Through("app.test"), "green-response")

    h.AdminPOST("/v1/services/myapp/rollback", nil)
    assertRequest(t, h.Through("app.test"), "blue-response")
}
```

### 9.3 その他

- **race detector**: CI で `go test -race ./...` 常時実行
- **カバレッジ下限**: domain パッケージ (`service`, `router`, `health`) 80%+
- **ベンチ**: `BenchmarkRouter_ServeHTTP` — snapshot reload 中の throughput 影響計測
- **static check**: `go vet`, `staticcheck`, `golangci-lint` を CI 必須化

---

## 10. プロジェクトメタデータ & リポレイアウト

### 10.1 基本情報

| 項目 | 値 |
|---|---|
| Go module | `github.com/crowdy/conoha-proxy` |
| Go 版 | 1.22+ |
| License | Apache-2.0 (conoha-cli と同様) |
| Copyright | `Copyright 2026 crowdy` |
| README 言語 | 日本語 (主) + 英語・韓国語 |
| Docker image | `ghcr.io/crowdy/conoha-proxy:v<version>` |

### 10.2 ディレクトリレイアウト

```
conoha-proxy/
├─ cmd/
│  └─ conoha-proxy/
│     └─ main.go                  # cobra root + subcommands
├─ internal/
│  ├─ adminapi/                   # C6
│  ├─ health/                     # C4
│  ├─ logging/                    # C7
│  ├─ router/                     # C1
│  ├─ service/                    # C3 (domain)
│  ├─ store/                      # C2 (bbolt)
│  └─ tls/                        # C5 (certmagic wrapper)
├─ docs/
│  ├─ architecture.md             # Section 3 アーキ図 (README 同期)
│  ├─ ops-runbook.md              # Section 7 フロー + Section 8 エラー対応
│  ├─ admin-api.md                # API reference
│  └─ superpowers/
│     └─ specs/
│        └─ 2026-04-20-conoha-proxy-design.md
├─ test/
│  └─ e2e/                        # Pebble + real HTTPS
├─ .github/
│  └─ workflows/
│     ├─ ci.yml
│     └─ release.yml              # goreleaser + docker publish
├─ Dockerfile                     # multi-stage scratch
├─ .goreleaser.yaml               # multi-arch
├─ go.mod
├─ go.sum
├─ LICENSE                        # Apache-2.0 全文
├─ NOTICES.md                     # third-party licenses (certmagic, bbolt, etc.)
├─ README.md                      # 日本語、Section 3 図を含む
├─ README-en.md
├─ README-ko.md
└─ Makefile                       # build, test, lint, docker
```

---

## 11. 依存ライブラリ (主要)

| ライブラリ | 用途 | License |
|---|---|---|
| `github.com/caddyserver/certmagic` | ACME 全ライフサイクル | Apache-2.0 |
| `go.etcd.io/bbolt` | 永続 KV | MIT |
| `github.com/spf13/cobra` | CLI framework | Apache-2.0 |
| `log/slog` (stdlib) | 構造化ログ | BSD |
| `net/http/httputil` (stdlib) | リバースプロキシ | BSD |
| (test) `github.com/stretchr/testify` | assert helpers | MIT |
| (test) `github.com/letsencrypt/pebble` | ACME テスト | MPL-2.0 |

すべて Apache-2.0 と互換。NOTICES.md に列挙。

---

## 12. アウト・オブ・スコープ (将来候補)

- DNS-01 チャレンジ (libdns ドライバ経由、wildcard 対応)
- Basic Auth / IP allowlist / rate limit
- Prometheus `/metrics`
- OpenTelemetry trace
- パスベースルーティング
- メンテナンスモード (503 + custom body)
- 複数インスタンス (L7 ロードバランサ下、state 共有)
- WebSocket / HTTP/3 明示サポート (net/http 既定で動く範囲は可)

---

## 13. ドキュメント生成アーティファクト (本スペックから派生)

実装フェーズで以下を生成・同期維持する:

- `README.md` — Section 3 配置図を含む
- `docs/architecture.md` — Section 3〜4 詳細
- `docs/ops-runbook.md` — Section 7〜8 運用手順
- `docs/admin-api.md` — Section 6 API リファレンス (+ OpenAPI 3 を後段で生成検討)
