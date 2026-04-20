# conoha-proxy

[![ci](https://github.com/crowdy/conoha-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/crowdy/conoha-proxy/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

> This is an English translation. The authoritative source is [README.md](README.md) (Japanese).

A Go reverse-proxy daemon for ConoHa VPS. Automatic HTTPS via Let's Encrypt, multi-service host-based routing, and blue/green deploys.

[日本語](README.md) | [한국어](README-ko.md)

## Features

- Single Go binary, shipped as a Docker image
- Automatic Let's Encrypt issuance and renewal (HTTP-01 challenge)
- Per-service blue/green target swap with drain
- Deploy gating via HTTP health probes
- Admin HTTP API over Unix socket or loopback TCP
- Structured JSON logs
- Apache-2.0

## Topology

See the diagram in [README.md](README.md#配置). The proxy runs as a Docker container on the VPS, terminating :80 / :443 and routing requests to local upstream containers. `conoha-cli` drives deploys over SSH via the admin socket.

## Quick start

```bash
docker run -d --name conoha-proxy \
  -p 80:80 -p 443:443 \
  -v conoha-proxy-data:/var/lib/conoha-proxy \
  -v /var/run/conoha-proxy.sock:/var/run/conoha-proxy.sock \
  ghcr.io/crowdy/conoha-proxy:latest \
  run --acme-email=admin@example.com

# Register a service
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services \
  -d '{"name":"myapp","hosts":["app.example.com"]}'

# First deploy
curl --unix-socket /var/run/conoha-proxy.sock http://admin/v1/services/myapp/deploy \
  -d '{"target_url":"http://127.0.0.1:9001"}'
```

## Documentation

- [docs/architecture.md](docs/architecture.md) — Internal architecture and components
- [docs/ops-runbook.md](docs/ops-runbook.md) — Operational procedures
- [docs/admin-api.md](docs/admin-api.md) — Admin HTTP API reference

## License

Apache-2.0 — see [LICENSE](LICENSE). Third-party dependencies are listed in [NOTICES.md](NOTICES.md).
