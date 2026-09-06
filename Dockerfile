# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.1 AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

ENV GOTOOLCHAIN=local

RUN --mount=type=bind,target=. \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    CGO_ENABLED=0 \
    GOOS="${TARGETOS}" \
    GOARCH="${TARGETARCH}" \
    go build \
    -mod=readonly \
    -trimpath \
    -buildvcs=false \
    -o /out/ \
    ./cmd/bootstrap-manager \
    ./cmd/control-plane-manager \
    ./cmd/infrastructure-manager \
    ./cmd/netboot-server


FROM gcr.io/distroless/static-debian12:nonroot AS bootstrap-manager

WORKDIR /

COPY --link --from=builder /out/bootstrap-manager /manager

USER nonroot:nonroot

ENTRYPOINT ["/manager"]


FROM gcr.io/distroless/static-debian12:nonroot AS control-plane-manager

WORKDIR /

COPY --link --from=builder /out/control-plane-manager /manager

USER nonroot:nonroot

ENTRYPOINT ["/manager"]


FROM gcr.io/distroless/static-debian12:nonroot AS infrastructure-manager

WORKDIR /

COPY --link --from=builder /out/infrastructure-manager /manager

USER nonroot:nonroot

ENTRYPOINT ["/manager"]


FROM gcr.io/distroless/static-debian12:nonroot AS netboot-server

WORKDIR /

COPY --link --from=builder /out/netboot-server /netboot-server

USER nonroot:nonroot

ENTRYPOINT ["/netboot-server"]
