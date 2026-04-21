# dockers_v2 Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate `.goreleaser.yaml` from the deprecated `dockers:` block to `dockers_v2:`, adding linux/arm64 to the published manifest, so `ghcr.io/crowdy/conoha-proxy:{{ .Version }}` + `:latest` continue to publish on every `v*` tag with zero deprecation warnings.

**Architecture:** Three tightly-coupled file changes shipped as one PR: (1) `.goreleaser.yaml` replaces `dockers:` with `dockers_v2:`, (2) `Dockerfile.release` uses `${TARGETPLATFORM}/conoha-proxy` for the binary path to match goreleaser v2's per-platform dist layout, (3) `.github/workflows/release.yml` adds QEMU + buildx setup because `dockers_v2` always uses buildx and we're now multi-arch. Verification runs locally via `goreleaser check` + `goreleaser release --snapshot --clean --skip=publish`, then end-to-end in CI via a pre-release tag before cutting the real release.

**Tech Stack:** goreleaser v2 (`dockers_v2` experimental, available v2.12+), docker buildx, GitHub Actions, distroless base image.

**Spec:** [docs/superpowers/specs/2026-04-21-dockers-v2-migration-design.md](../specs/2026-04-21-dockers-v2-migration-design.md)

**Issue:** https://github.com/crowdy/conoha-proxy/issues/3

---

## File Structure

Three files change. No new files.

| File                                  | Responsibility                                                  | Change             |
| ------------------------------------- | --------------------------------------------------------------- | ------------------ |
| `.goreleaser.yaml`                    | goreleaser config — declares builds, archives, docker images    | Replace `dockers:` → `dockers_v2:` |
| `Dockerfile.release`                  | Release-only Dockerfile consumed by goreleaser                  | Update `COPY` path + header comment |
| `.github/workflows/release.yml`       | Release CI workflow triggered on `v*` tags                      | Add 2 setup steps  |

No test files change. The "tests" are `goreleaser check` + snapshot build + image run. This is infrastructure config — correctness is proved by running the real tool end-to-end, not by unit tests.

---

## Task 1: Migrate `.goreleaser.yaml` to `dockers_v2`

**Files:**
- Modify: `.goreleaser.yaml:26-44` (replace the `dockers:` block)

This task alone will NOT produce a working build — the `Dockerfile.release` COPY path and CI buildx setup are needed too. We verify end-to-end after Task 3. But we commit per-task for reviewability.

- [ ] **Step 1: Open `.goreleaser.yaml` and confirm the current `dockers:` block matches the expected pre-state**

Run:

```bash
sed -n '26,44p' .goreleaser.yaml
```

Expected: the block beginning `dockers:` and ending with `- "--platform=linux/amd64"`. If it doesn't match, stop and investigate — the file has drifted from the spec's assumptions.

- [ ] **Step 2: Replace the `dockers:` block with `dockers_v2:`**

Use an Edit to replace the entire `dockers:` block (starting at `dockers:` through the trailing `- "--platform=linux/amd64"` line, inclusive) with:

```yaml
dockers_v2:
  - id: conoha-proxy
    ids: [conoha-proxy]
    dockerfile: Dockerfile.release
    # Pull the linux binaries goreleaser already built under the
    # `conoha-proxy` id. `ids` filters out the darwin builds so they
    # don't end up in the docker build context. `platforms` drives
    # the multi-arch manifest — builds: already produces both
    # linux/amd64 and linux/arm64 binaries.
    images:
      - ghcr.io/crowdy/conoha-proxy
    tags:
      - "{{ .Version }}"
      - "latest"
    platforms:
      - linux/amd64
      - linux/arm64
```

Everything else in the file (version, project_name, builds, archives, checksum) is untouched.

- [ ] **Step 3: Confirm the result is syntactically valid YAML and matches expectations**

Run:

```bash
grep -n '^dockers' .goreleaser.yaml
grep -n 'docker_manifests' .goreleaser.yaml
```

Expected:
- First command prints exactly one line: `26:dockers_v2:` (or similar — line number may differ, but only one match).
- Second command prints nothing (no `docker_manifests` block anywhere in the file).

Then:

```bash
python3 -c "import yaml; yaml.safe_load(open('.goreleaser.yaml'))"
```

Expected: no output (exit 0). If it errors, fix the YAML indentation and re-run before proceeding.

- [ ] **Step 4: Run `goreleaser check`**

Run:

```bash
goreleaser check
```

Expected: exit 0. The output may still contain warnings about `Dockerfile.release` expecting a specific context layout — those are resolved in Task 2. But the `dockers and docker_manifests are being phased out` deprecation warning MUST be gone.

If the deprecation warning is still there, Task 1 is not done — recheck the file for a lingering `dockers:` or `docker_manifests:` key.

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yaml
git commit -m "$(cat <<'EOF'
chore(release): migrate .goreleaser.yaml dockers → dockers_v2

Drops the deprecated dockers:/docker_manifests: schema in favor of
dockers_v2:, which consolidates image/tag/platform config and produces a
multi-arch manifest in one pass. Adds linux/arm64 to the published
manifest since builds: already produces that binary.

Fixes #3 (partial — see follow-up commits for Dockerfile.release and
release.yml changes).
EOF
)"
```

Expected: commit succeeds. `goreleaser check` still clean (no deprecation). Snapshot build is NOT expected to work yet — that's Task 2+3.

---

## Task 2: Update `Dockerfile.release` for v2 context layout

**Files:**
- Modify: `Dockerfile.release` (full replacement — it's 22 lines)

`dockers_v2` stages the build context as `<goos>/<goarch>/<binary>` and buildx injects `TARGETPLATFORM` per platform. The COPY path must match that structure.

- [ ] **Step 1: Replace the file contents**

Overwrite `Dockerfile.release` with exactly this content:

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

- [ ] **Step 2: Diff against the previous version to confirm only intended lines changed**

Run:

```bash
git diff Dockerfile.release
```

Expected changes vs previous version:
- Header comment rewritten (reflects `dockers_v2:` + TARGETPLATFORM).
- `FROM gcr.io/distroless/static:nonroot` → `FROM --platform=$TARGETPLATFORM gcr.io/distroless/static:nonroot`.
- New line: `ARG TARGETPLATFORM`.
- `COPY conoha-proxy /usr/local/bin/conoha-proxy` → `COPY ${TARGETPLATFORM}/conoha-proxy /usr/local/bin/conoha-proxy`.
- Everything else (`USER`, `EXPOSE`, `VOLUME`, `ENTRYPOINT`, `CMD`, `STOPSIGNAL`) unchanged.

If anything else changed (extra whitespace, reordered directives), revert and redo the write.

- [ ] **Step 3: Run the full local snapshot verification**

This is the moment of truth for Tasks 1 + 2 together.

```bash
goreleaser release --snapshot --clean --skip=publish
```

Expected: exit 0. Stderr/stdout shows:
- Binaries built for darwin amd64, darwin arm64, linux amd64, linux arm64.
- Two docker images built: `ghcr.io/crowdy/conoha-proxy:<snapshot>-amd64` and `…-arm64`. (In snapshot mode v2 emits platform-suffixed tags instead of a manifest, because manifests require pushing.)

If the build fails with a `COPY` error, the context layout is not what we expected — inspect `dist/` to see where goreleaser actually placed the binaries, and adjust the Dockerfile or filename accordingly.

- [ ] **Step 4: Smoke-test both snapshot images**

Pick the snapshot tag from the previous step's output (it looks like `0.1.1-next-<shortsha>` or similar). Then run:

```bash
# Replace <SNAPSHOT_TAG> with the actual tag from the goreleaser output.
SNAP=<SNAPSHOT_TAG>
docker run --rm ghcr.io/crowdy/conoha-proxy:${SNAP}-amd64 version
docker run --rm --platform=linux/arm64 \
  ghcr.io/crowdy/conoha-proxy:${SNAP}-arm64 version
```

Expected: each command prints three lines — `version:`, `commit:`, `buildDate:` — populated with the snapshot values (version field contains the snapshot tag, commit field contains the short SHA of HEAD, buildDate is today in RFC3339).

If the arm64 run fails with `exec format error`, the host is missing QEMU binfmt handlers — install `qemu-user-static` (Debian/Ubuntu) or the equivalent for your distro, or skip the arm64 smoke and rely on CI verification in Task 3.

- [ ] **Step 5: Confirm goreleaser check still clean and no deprecation warnings anywhere**

```bash
goreleaser check 2>&1 | tee /tmp/goreleaser-check.log
grep -i "deprecat" /tmp/goreleaser-check.log || echo "NO DEPRECATION WARNINGS"
```

Expected: final line prints `NO DEPRECATION WARNINGS`. If grep matched anything, the migration is incomplete — investigate before proceeding.

- [ ] **Step 6: Commit**

```bash
git add Dockerfile.release
git commit -m "$(cat <<'EOF'
chore(release): adapt Dockerfile.release to dockers_v2 context layout

goreleaser's dockers_v2 build context is laid out as <goos>/<goarch>/<binary>
and buildx injects TARGETPLATFORM per-platform. Update the COPY path to
match, and pin the base image selection to $TARGETPLATFORM so cross-arch
builds resolve the correct distroless variant deterministically.

Follow-up to the dockers_v2 schema swap; #3.
EOF
)"
```

---

## Task 3: Add buildx setup to the release workflow

**Files:**
- Modify: `.github/workflows/release.yml` (insert 2 uses: steps)

`dockers_v2` always uses buildx. A CI job without a configured buildx builder will fail. We also need QEMU so buildx can validate arm64 layers on the amd64 runner.

- [ ] **Step 1: Insert the two setup steps between `setup-go` and `login-action`**

Edit `.github/workflows/release.yml`. Find the sequence:

```yaml
      - uses: actions/setup-go@v5
        with: { go-version: "1.24" }
      - uses: docker/login-action@v3
```

Replace it with:

```yaml
      - uses: actions/setup-go@v5
        with: { go-version: "1.24" }
      - uses: docker/setup-qemu-action@v3
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
```

No other line in the workflow changes.

- [ ] **Step 2: Diff the workflow to verify the surgical change**

Run:

```bash
git diff .github/workflows/release.yml
```

Expected: exactly two new lines added (the two `uses:` lines), no other changes. If other lines moved (e.g. indentation normalization), revert and redo.

- [ ] **Step 3: Validate the workflow syntactically**

Run:

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))"
```

Expected: no output (exit 0).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "$(cat <<'EOF'
ci(release): add QEMU + buildx setup for dockers_v2 multi-arch build

dockers_v2 always uses docker buildx (not the legacy docker build), so
the runner needs a configured buildx builder. Adding setup-qemu-action
too registers binfmt handlers for cross-arch layer packaging, which we
need now that the manifest includes linux/arm64.

Completes the dockers_v2 migration; #3.
EOF
)"
```

---

## Task 4: End-to-end CI verification via rc tag

**Files:** none (this task runs against the real GHA + GHCR pipeline).

This task exercises the full release workflow before we cut a real `v0.1.2`. If this fails, we debug on the rc tag and re-tag, keeping the real release clean.

- [ ] **Step 1: Push the branch and open a PR**

Assumes the three commits from Tasks 1–3 are on a topic branch (e.g. `chore/dockers-v2`).

```bash
git push -u origin chore/dockers-v2
gh pr create --title "chore(release): migrate goreleaser dockers: → dockers_v2:" --body "$(cat <<'EOF'
## Summary
- Replace deprecated `dockers:` block in `.goreleaser.yaml` with `dockers_v2:`.
- Update `Dockerfile.release` COPY path to match v2's `<goos>/<goarch>/<binary>` context layout and pin base image to `$TARGETPLATFORM`.
- Add `setup-qemu-action` + `setup-buildx-action` to the release workflow — `dockers_v2` always uses buildx.
- Add linux/arm64 to the published manifest (builds: already produces the binary).

Closes #3.

## Test plan
- [x] `goreleaser check` — zero deprecation warnings.
- [x] `goreleaser release --snapshot --clean --skip=publish` succeeds, both amd64 and arm64 snapshot images run and print `version` correctly.
- [ ] Post-merge: push a `v*-rc1` tag, verify `docker manifest inspect` returns two platforms, and both `:rc1` and `:latest` pull and run.

Spec: [docs/superpowers/specs/2026-04-21-dockers-v2-migration-design.md](docs/superpowers/specs/2026-04-21-dockers-v2-migration-design.md)
EOF
)"
```

Expected: PR URL returned. Wait for the `ci` workflow to pass on the PR.

**STOP here and wait for user approval of the PR before proceeding.** The rc tag verification requires merging first, and merging is a user decision.

- [ ] **Step 2: After merge, push an rc tag against main**

Once the PR is merged to `main`:

```bash
git checkout main
git pull --ff-only
# Replace X.Y.Z with the next release version.
git tag v0.1.2-rc1
git push origin v0.1.2-rc1
```

Expected: the `release` workflow starts on the tag. Watch it:

```bash
gh run watch
```

- [ ] **Step 3: Verify the published image**

After the workflow completes:

```bash
docker pull ghcr.io/crowdy/conoha-proxy:0.1.2-rc1
docker manifest inspect ghcr.io/crowdy/conoha-proxy:0.1.2-rc1 \
  | jq '[.manifests[] | {platform: .platform.os + "/" + .platform.architecture, digest: .digest}]'
docker run --rm ghcr.io/crowdy/conoha-proxy:0.1.2-rc1 version
docker pull ghcr.io/crowdy/conoha-proxy:latest
docker run --rm ghcr.io/crowdy/conoha-proxy:latest version
```

Expected:
- `docker manifest inspect` prints a JSON array with two entries: `{"platform": "linux/amd64", ...}` and `{"platform": "linux/arm64", ...}`.
- Both `version` runs print three lines (version / commit / buildDate).
- `version` field matches `0.1.2-rc1` (without the `v` prefix — goreleaser strips it by default).

- [ ] **Step 4: Cut the real release tag**

Only after Step 3 passes cleanly:

```bash
git tag v0.1.2
git push origin v0.1.2
gh run watch
```

Then repeat Step 3's verification with `0.1.2` instead of `0.1.2-rc1`. Close #3 once done.

---

## Self-review notes

- **Spec coverage:**
  - Spec §Design → Task 1 (yaml) + Task 2 (Dockerfile) + Task 3 (workflow). ✓
  - Spec §Verification local → Task 2 Steps 3–5. ✓
  - Spec §Verification CI → Task 4 Steps 2–4. ✓
  - Spec §Rollback → not a task, but mentioned in PR body; Task 1 commit message notes partial fix so reverters know all 3 commits must be reverted together. Acceptable — rollback is a human action.
- **Placeholder scan:** one intentional placeholder remains — the snapshot tag in Task 2 Step 4 (`<SNAPSHOT_TAG>`). It's marked inline and explained, because the tag is generated by goreleaser at run time and cannot be predicted. Acceptable.
- **Type consistency:** N/A — no code, just YAML keys. `dockers_v2`, `images`, `tags`, `platforms`, `ids`, `dockerfile` spellings are consistent throughout the plan and match the goreleaser v2 docs.
