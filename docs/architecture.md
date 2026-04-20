# アーキテクチャ

conoha-proxy の内部構造を、実装パッケージ単位でまとめたリファレンス。運用者とコミッタ向け。

## プロセスモデル

単一の Go プロセスとして動作する。外部から見えるソケットは次の 3 種。

```
conoha-proxy (single Go binary)
├─ HTTP  :80                             ACME http-01 challenge + HTTPS redirect
├─ HTTPS :443                            TLS termination + reverse proxy
├─ Admin <data-dir>/admin.sock           Unix socket (既定)
│        127.0.0.1:9999                  ループバック TCP (任意)
└─ Background goroutines:
   └─ certmagic manager (renewal)
```

(各ターゲットへの継続的 health worker と、独立した persistence flusher は MVP スコープ外。`internal/health.ProbeOnce` による同期 probe だけで deploy 合否を判定する。仕様書 §12 参照。)

Admin 受け口は `--admin-socket` と `--admin-tcp` のどちらか一方、もしくは両方を指定できる。TCP を指定した場合、起動時にループバックアドレスへの解決が確認されないと致命エラーで停止する (`internal/adminapi/server.go: requireLoopback`)。

## デプロイ配置

VPS 上の Docker コンテナとして動作し、同一ホスト内の upstream コンテナへルーティングする前提。

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

コンテナ自体の起動・停止・イメージ pull は本プロセスの責務外。`conoha-cli` などの呼び出し側が SSH 経由で行う。

## コンポーネント

ディレクトリは `internal/` 配下。すべて内部パッケージで、外部からの import は想定しない。

| パッケージ | 主なファイル | 責務 |
|---|---|---|
| `internal/router` | `router.go`, `snapshot.go` | Host ヘッダー → Service/active target のマッチ、`httputil.ReverseProxy` でのラップ、atomic な snapshot 置き換え |
| `internal/store` | `store.go`, `bbolt.go` | bbolt 上の永続 KV。トランザクションと JSON シリアライズ |
| `internal/service` | `service.go`, `phase.go` | ドメイン型 (`Service`, `Target`, `HealthPolicy`) と外部観測可能な Phase の導出 |
| `internal/health` | `health.go` | HTTP probe (`ProbeOnce`) による同期ヘルスチェック |
| `internal/tls` | `certmanager.go` | certmagic ラッパー。ACME の発行・更新・ストレージを隠蔽 |
| `internal/adminapi` | `handlers.go`, `server.go`, `errors.go` | Admin HTTP API のハンドラ実装とリスナー (Unix / loopback TCP) |
| `internal/logging` | `slog.go` | 構造化 JSON ロガー (`log/slog`) の初期化 |
| `cmd/conoha-proxy` | `main.go` | cobra ベースのエントリーポイント。各コンポーネントを `main` で手動配線 |

### 主要インターフェース

```go
// internal/router
type Router interface {
    ServeHTTP(http.ResponseWriter, *http.Request)
    Reload(snapshot ServiceSnapshot) error  // atomic swap
}

// internal/store
type Store interface {
    LoadAll(ctx context.Context) ([]Service, error)
    SaveService(ctx context.Context, svc Service) error
    DeleteService(ctx context.Context, name string) error
}

// internal/health
type Checker interface {
    ProbeOnce(ctx context.Context, t Target, p HealthPolicy) error
}

// internal/tls
type CertManager interface {
    GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error)
    ManageDomains(domains []string) error
}
```

## 依存方向

依存は一方向に保ち、循環を作らない。すべての I/O は interface 越しで、テストでは fake に差し替える。

```
cmd ─▶ adminapi ─▶ service ─▶ store
          │           ▲
          ├──────────▶ router ◀──────── tls
          │
          └─────────────▶ health (synchronous ProbeOnce during deploy)
```

DI コンテナやミドルウェア chain フレームワークは導入しない。`cmd/conoha-proxy/main.go` の `runProxy` が組み立てる (`main.go:102-122`)。

## データモデル

### bbolt レイアウト

```
/var/lib/conoha-proxy/state.db
├─ services/                       (bucket)
│   └─ <service-name> → JSON-encoded Service
└─ meta/                           (bucket)
    ├─ schema_version → "1"
    └─ instance_id    → UUID
```

証明書ストアは別ディレクトリ `/var/lib/conoha-proxy/certs/` で、certmagic 既定のレイアウトをそのまま使う。

### Service JSON

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
  "drain_deadline": "2026-04-20T10:30:30Z",
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

`drain_ms` は deploy リクエストに含めるパラメータで、`drain_deadline` にだけ反映される (Service 本体には保持しない)。

### Phase 遷移

外部観測可能な Phase は `configured`, `live`, `swapping` の 3 種のみ。内部の probing は API 呼び出し内部で完結するため、routing 層からは見えない (`internal/service/phase.go`)。

```
  [ 未登録 ]
     │ upsert
     ▼
  [ Configured ]   active=nil, draining=nil
     │ deploy {new_target}
     ▼
  (ProbeOnce 実行中 — API 内部フェーズ、観測不可)
     │ probe 成功                  │ probe 失敗
     ▼                              │
  [ Live ]                         └→ Configured 復帰、new は破棄
     │ deploy {new_target}
     ▼
  (ProbeOnce)
     │ 成功                         │ 失敗
     ▼                              │
  [ Swapping ]                     └→ active=old 維持、new は破棄
  (active=new, draining=old, deadline=now+drain_ms)
     │ drain deadline 経過
     ▼
  [ Live ]         active=new, draining=nil
```

## 不変式

実装と運用が依拠する重要な性質。

1. `active_target` が nil の間、該当ホストへの要求は **503** を返す。
2. 新しいターゲットは probe を通過するまで**決して**トラフィックを受けない。probing は admin API の呼び出しスレッドで同期的に完了させ、並行挿入を許さない。
3. スワップは原子的に行う (`store` のトランザクション + `router.Reload(snapshot)` を 1 セット)。途中で落ちても snapshot が中間状態を持つことはない。
4. Probing と SwapProbing は観測 API では単一の概念として扱わない。Phase は `configured | live | swapping` の 3 値に集約する (Section 5.4 仕様書)。
5. `drain_ms` は deploy 毎のパラメータで、Service 自体には保持しない。次回 deploy で上書き可能。
6. Host ヘッダー比較は**完全一致 (大文字小文字無視、ポート除外)**。ワイルドカードは MVP 非対応。

## graceful shutdown

SIGTERM を受けると次の順で終了する。

1. HTTP / HTTPS / Admin リスナーの `Accept` を停止
2. in-flight 要求を `ReadHeaderTimeout` + `WriteTimeout` 内で完走させる
3. health worker と certmagic renewer を停止
4. store の最終 flush
5. `exit 0`

Docker では `STOPSIGNAL SIGTERM` と `--stop-timeout 60` を併用する前提。

## 設計上の非スコープ (YAGNI)

- ミドルウェア chain フレームワーク (Chi 等) — `http.Handler` で十分
- ORM — bbolt + JSON で足りる
- DI コンテナ — `main.go` で手動配線
- gRPC Admin API — HTTP + JSON がデバッグしやすい
- 分散・複数インスタンス — 単一プロセス前提
- パスベースルーティング / ワイルドカードホスト — DNS-01 対応と同時期に再検討
