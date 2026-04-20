# syntax=docker/dockerfile:1.7
FROM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=""
ARG BUILD_DATE=""
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.buildDate=${BUILD_DATE} \
      -X github.com/crowdy/conoha-proxy/internal/adminapi.version=${VERSION}" \
    -o /out/conoha-proxy \
    ./cmd/conoha-proxy

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/conoha-proxy /usr/local/bin/conoha-proxy
USER nonroot:nonroot
EXPOSE 80 443
VOLUME ["/var/lib/conoha-proxy"]
ENTRYPOINT ["/usr/local/bin/conoha-proxy"]
CMD ["run"]
STOPSIGNAL SIGTERM
