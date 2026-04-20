# conoha-proxy

[![ci](https://github.com/crowdy/conoha-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/crowdy/conoha-proxy/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

> 이 문서는 한국어 번역입니다. 원본은 [README.md](README.md) (일본어) 입니다.

ConoHa VPS 용 Go 리버스 프록시 데몬. Let's Encrypt 기반 자동 HTTPS, 여러 서비스의 도메인별 라우팅, blue/green 배포를 제공합니다.

[日本語](README.md) | [English](README-en.md)

## 특징

- 단일 Go 바이너리, Docker 이미지로 배포
- Let's Encrypt 자동 발급 및 갱신 (HTTP-01 challenge)
- 서비스별 blue/green 타깃 스왑 및 drain
- HTTP 헬스체크 기반의 배포 성공/실패 판정
- Admin HTTP API (Unix 소켓 또는 loopback TCP)
- 구조화된 JSON 로그
- Apache-2.0

## 배치

상세 구성도는 [README.md](README.md#배치) 를 참고하세요. 프록시는 VPS 위에 Docker 컨테이너로 동작하여 :80 / :443 을 종단 처리하고, 로컬 upstream 컨테이너로 요청을 분배합니다. `conoha-cli` 가 SSH 를 통해 admin 소켓으로 배포를 수행합니다.

## 빠른 시작

Admin Unix 소켓은 **data volume 내부** (`/var/lib/conoha-proxy/admin.sock`) 에 생성됩니다. distroless `nonroot` 사용자가 확실하게 쓸 수 있는 경로가 거기뿐이기 때문입니다. 호스트에서 조작할 때는 해당 디렉터리를 bind-mount 하세요.

```bash
# 호스트에 디렉터리를 만들고 컨테이너 내 nonroot (uid 65532) 에게 소유권 부여
sudo mkdir -p /var/lib/conoha-proxy
sudo chown 65532:65532 /var/lib/conoha-proxy

docker run -d --name conoha-proxy \
  -p 80:80 -p 443:443 \
  -v /var/lib/conoha-proxy:/var/lib/conoha-proxy \
  ghcr.io/crowdy/conoha-proxy:latest \
  run --acme-email=admin@example.com

# 서비스 등록 (호스트에서 동일 경로로 접근)
curl --unix-socket /var/lib/conoha-proxy/admin.sock http://admin/v1/services \
  -d '{"name":"myapp","hosts":["app.example.com"]}'

# 최초 배포
curl --unix-socket /var/lib/conoha-proxy/admin.sock http://admin/v1/services/myapp/deploy \
  -d '{"target_url":"http://127.0.0.1:9001"}'
```

## 문서

- [docs/architecture.md](docs/architecture.md) — 내부 아키텍처 및 구성 요소
- [docs/ops-runbook.md](docs/ops-runbook.md) — 운영 절차 (배포, 롤백, 백업)
- [docs/admin-api.md](docs/admin-api.md) — Admin HTTP API 레퍼런스

## 라이선스

Apache-2.0 — [LICENSE](LICENSE) 참조. 서드파티 의존성은 [NOTICES.md](NOTICES.md) 를 확인하세요.
