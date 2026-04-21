---
issue: https://github.com/crowdy/conoha-proxy/issues/3
status: approved
---

# dockers → dockers_v2 migration (goreleaser)

## Context

Running `goreleaser check` / `goreleaser release --snapshot` on the current
`.goreleaser.yaml` emits:

```
• dockers and docker_manifests are being phased out and will eventually
  be replaced by dockers_v2, check https://goreleaser.com/deprecations#dockers
  for more info
```

The legacy `dockers:` block works today (v0.1.1 shipped via it) but will be
removed in a future goreleaser major. Migrate now, on our own schedule,
before a runner-image / goreleaser version bump forces a hard break.

Driving issue: [#3](https://github.com/crowdy/conoha-proxy/issues/3).

## Goals

1. Replace `dockers:` in `.goreleaser.yaml` with an equivalent `dockers_v2:`
   block per the [migration doc](https://goreleaser.com/deprecations#dockers)
   and the [`dockers_v2` reference](https://goreleaser.com/customization/dockers_v2/).
2. Publish the same two tags as today: `ghcr.io/crowdy/conoha-proxy:{{ .Version }}`
   and `:latest`.
3. Upgrade to a multi-arch manifest (linux/amd64 + linux/arm64) while we're
   here — the `builds:` block already produces both binaries, so it is
   effectively free. Approved per issue's "nice-to-have" note.
4. `goreleaser check` must emit zero deprecation warnings after the change.

## Non-goals

- Do not change the `Dockerfile` / `Dockerfile.release` split from PR #2.
  Only the `COPY` path inside `Dockerfile.release` changes to match v2's
  context layout.
- Do not touch the CI `docker build .` sanity-check job — it uses the
  top-level `Dockerfile`, not goreleaser.
- Do not add OCI labels, SBOM-disable flags, or other v2 niceties beyond
  what the migration requires. SBOM generation is accepted as a v2 default
  (net-positive, not a regression).
- Do not pin `goreleaser-action`'s `version: latest` in this change. Pinning
  is a separate reproducibility concern.

## Design

### Files touched

1. `.goreleaser.yaml` — replace `dockers:` block with `dockers_v2:`.
2. `Dockerfile.release` — change `COPY` path to use `${TARGETPLATFORM}` and
   update header comment.
3. `.github/workflows/release.yml` — add `setup-qemu-action` +
   `setup-buildx-action` steps.

These three changes are tightly coupled: the Dockerfile path format must
match goreleaser's dist/ layout for the goreleaser version in use, and the
CI runner must have buildx available because `dockers_v2` always uses it.
Partial revert is not safe (see Rollback).

### `.goreleaser.yaml`

Replace the existing `dockers:` block (lines 26–44 of the current file)
with:

```yaml
dockers_v2:
  - id: conoha-proxy
    ids: [conoha-proxy]
    dockerfile: Dockerfile.release
    images:
      - ghcr.io/crowdy/conoha-proxy
    tags:
      - "{{ .Version }}"
      - "latest"
    platforms:
      - linux/amd64
      - linux/arm64
```

Everything else (`version`, `project_name`, `builds`, `archives`,
`checksum`) is unchanged.

Mapping from v1:

| v1 (current)                                         | v2 (new)                      |
| ---------------------------------------------------- | ----------------------------- |
| `image_templates: ["…:{{.Version}}", "…:latest"]`    | `images: […]` + `tags: […]`   |
| `dockerfile: Dockerfile.release`                     | unchanged                     |
| `ids: [conoha-proxy]`                                | unchanged                     |
| `goos: linux` + `goarch: amd64` + `--platform=…`     | `platforms: [linux/amd64, linux/arm64]` |
| `use: buildx`                                        | implicit (v2 always uses buildx) |
| separate `docker_manifests:` for multi-arch         | not needed — v2 builds the manifest in one pass |

### `Dockerfile.release`

`dockers_v2` lays out its build context as `<goos>/<goarch>/<binary>` (i.e.
`linux/amd64/conoha-proxy`, `linux/arm64/conoha-proxy`) and buildx injects
`TARGETPLATFORM` per-platform. Adjust the `COPY` path to match and pin the
base image selection to the target platform explicitly.

New content:

```dockerfile
# syntax=docker/dockerfile:1.7
#
# Consumed by goreleaser only (.goreleaser.yaml `dockers_v2:` references
# this file). goreleaser's dockers_v2 build context is a dist/ staging
# directory laid out as <goos>/<goarch>/<binary> so that buildx can
# consume it for multi-arch manifest builds. TARGETPLATFORM is injected
# by buildx per-platform (e.g. "linux/amd64", "linux/arm64") and is used
# to pick the right binary. For from-source builds (CI, local dev) use
# the top-level Dockerfile instead.
FROM --platform=$TARGETPLATFORM gcr.io/distroless/static:nonroot
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/conoha-proxy /usr/local/bin/conoha-proxy
USER nonroot:nonroot
EXPOSE 80 443
VOLUME ["/var/lib/conoha-proxy"]
ENTRYPOINT ["/usr/local/bin/conoha-proxy"]
CMD ["run"]
STOPSIGNAL SIGTERM
```

Change summary vs current file:
- `FROM gcr.io/distroless/static:nonroot` → prefix with `--platform=$TARGETPLATFORM`.
- Add `ARG TARGETPLATFORM` immediately after `FROM`.
- `COPY conoha-proxy …` → `COPY ${TARGETPLATFORM}/conoha-proxy …`.
- Header comment rewritten to describe the v2 context layout.

### `.github/workflows/release.yml`

Insert two setup steps between `setup-go` and `login-action`:

```yaml
      - uses: docker/setup-qemu-action@v3
      - uses: docker/setup-buildx-action@v3
```

Rationale:
- `setup-qemu-action` registers binfmt handlers so buildx can validate and
  package arm64 layers on an amd64 runner. Our Dockerfile only COPYs a
  pre-built binary (no `RUN` during image build) so QEMU is not strictly
  required for execution, but keeping it makes base-image resolution and
  platform validation robust across future goreleaser versions.
- `setup-buildx-action` creates a dedicated buildx builder. `dockers_v2`
  always uses buildx, not the legacy `docker build`, so a configured
  builder must be present.

No other workflow change. `goreleaser-action@v6` with `version: latest`
already resolves to a goreleaser release that supports `dockers_v2`
(experimental since v2.12).

## Verification

### Local (pre-merge, on the topic branch)

```bash
# 1. Config sanity + zero deprecation noise.
goreleaser check

# 2. Full snapshot build, no publish, output images land in local daemon.
goreleaser release --snapshot --clean --skip=publish

# 3. Per-platform smoke of the snapshot images.
#    In snapshot mode v2 emits platform-suffixed tags (no manifest,
#    because manifests require pushing).
SNAP=$(yq -r '.version' dist/metadata.json 2>/dev/null || echo "<snapshot-version>")
docker run --rm ghcr.io/crowdy/conoha-proxy:${SNAP}-amd64 version
docker run --rm --platform=linux/arm64 \
  ghcr.io/crowdy/conoha-proxy:${SNAP}-arm64 version
```

Accept when:
- `goreleaser check` stderr contains no `dockers and docker_manifests are
  being phased out` line.
- Both snapshot images print `version:`, `commit:`, and `buildDate:` lines
  carrying the snapshot values.
- `dist/` contains both a linux/amd64 and linux/arm64 `conoha-proxy`
  binary under goreleaser's per-build directories (exact directory names
  depend on the goreleaser version; checking for `file dist/**/conoha-proxy`
  returning two ELF files — one `x86-64` and one `aarch64` — is enough).

### CI (post-merge, before the real release tag)

Push a release-candidate tag, e.g. `v0.1.2-rc1`, to trigger the release
workflow end-to-end, then:

```bash
docker pull ghcr.io/crowdy/conoha-proxy:0.1.2-rc1
docker manifest inspect ghcr.io/crowdy/conoha-proxy:0.1.2-rc1 \
  | jq '.manifests[] | {platform: .platform, digest: .digest}'
docker run --rm ghcr.io/crowdy/conoha-proxy:0.1.2-rc1 version
docker pull ghcr.io/crowdy/conoha-proxy:latest
docker run --rm ghcr.io/crowdy/conoha-proxy:latest version
```

Accept when:
- `docker manifest inspect` returns two entries: `linux/amd64` and
  `linux/arm64`.
- Both `:0.1.2-rc1` and `:latest` exist in GHCR.
- `version` subcommand prints the expected tag.

Cut the real `v0.1.2` tag only after the rc tag verifies.

## Rollback

If any of the above fails in a way we can't quickly patch, revert the
three files as a single commit:

- `.goreleaser.yaml`
- `Dockerfile.release`
- `.github/workflows/release.yml`

Partial revert is unsafe — the v2 Dockerfile's `COPY ${TARGETPLATFORM}/…`
path is incompatible with the v1 context layout, and `dockers_v2` requires
buildx set up by the workflow. Revert together or not at all.

## Risks

- **goreleaser `version: latest` drift.** `dockers_v2` is experimental
  as of v2.12. A future breaking rename in the v2 schema would affect this
  release. Mitigated by the rc-tag verification gate before each real
  release, and by the explicit rollback plan. Pinning the goreleaser
  version is out of scope here.
- **QEMU emulation failure on GHA runner images.** If a future runner
  ships without binfmt support, `setup-qemu-action` will fail loudly
  before `goreleaser` runs — easy to detect and revert.
- **First multi-arch release doubles image-publish bytes to GHCR.** Not a
  problem at our scale, noted only for completeness.
