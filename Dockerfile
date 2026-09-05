# syntax=docker/dockerfile:1
FROM golang:1.27.1 AS builder
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
COPY controller/ controller/
COPY host/ host/
COPY talos/ talos/
COPY bootstrap/ bootstrap/
COPY controlplane/ controlplane/
COPY boot/ boot/
COPY extensions/ extensions/
COPY utils/ utils/
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager ./cmd/controller-manager

FROM gcr.io/distroless/static:nonroot
WORKDIR /

COPY --from=builder /workspace/manager /manager

USER 65532:65532

ENTRYPOINT ["/manager"]
