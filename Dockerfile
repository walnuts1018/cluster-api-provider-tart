# syntax=docker/dockerfile:1
FROM golang:1.26.5 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download -x

COPY cmd/ cmd/
COPY api/ api/
COPY artifact/ artifact/
COPY domain/ domain/
COPY dto/ dto/
COPY infrastructure/ infrastructure/
COPY utils/ utils/
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager ./cmd/controller-manager && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o credential-init ./cmd/provisioning-credential-init

FROM debian:13.6-slim AS capabilities

RUN --mount=type=cache,target=/var/lib/apt,sharing=locked \
    --mount=type=cache,target=/var/cache/apt,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends libcap2-bin

COPY --from=builder /workspace/manager /manager
COPY --from=builder /workspace/credential-init /credential-init
RUN setcap 'cap_net_bind_service=+ep' /manager

FROM gcr.io/distroless/static:nonroot
WORKDIR /

COPY --from=capabilities /manager /manager
COPY --from=capabilities /credential-init /credential-init

USER 65532:65532

ENTRYPOINT ["/manager"]
