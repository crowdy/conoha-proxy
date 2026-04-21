# syntax=docker/dockerfile:1.7
#
# Consumed by goreleaser (see .goreleaser.yaml `dockers:`). goreleaser's
# docker build context is the release `dist/` staging directory, which
# contains only the pre-built binary (and any `extra_files`) — NOT the
# source tree. Running a multi-stage Go builder here therefore fails at
# `COPY go.mod go.sum ./` because those files are not in the context.
#
# Copy the binary goreleaser already built (with the correct ldflags —
# version/commit/buildDate — from the .goreleaser.yaml `builds` block)
# into a distroless runtime image.
FROM gcr.io/distroless/static:nonroot
COPY conoha-proxy /usr/local/bin/conoha-proxy
USER nonroot:nonroot
EXPOSE 80 443
VOLUME ["/var/lib/conoha-proxy"]
ENTRYPOINT ["/usr/local/bin/conoha-proxy"]
CMD ["run"]
STOPSIGNAL SIGTERM
