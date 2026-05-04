# Copyright 2026.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Stage 1: Build iPXE
FROM --platform=$BUILDPLATFORM alpine:3.20 AS builder

# Install build dependencies
RUN apk add --no-cache \
    git \
    make \
    gcc \
    musl-dev \
    binutils \
    perl \
    bash \
    xz-dev \
    mtools \
    xorriso

# Clone iPXE repository
RUN git clone --depth 1 https://github.com/ipxe/ipxe.git /ipxe

WORKDIR /ipxe/src

ARG TARGETARCH

# Build iPXE for UEFI
RUN if [ "$TARGETARCH" = "amd64" ]; then \
        make bin-x86_64-efi/ipxe.efi && cp bin-x86_64-efi/ipxe.efi /ipxe.efi; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
        make bin-arm64-efi/ipxe.efi && cp bin-arm64-efi/ipxe.efi /ipxe.efi; \
    else \
        echo "Unsupported architecture: $TARGETARCH"; exit 1; \
    fi

# Stage 2: Final Artifact
FROM scratch

COPY --from=builder /ipxe/COPYING /LICENSE
COPY --from=builder /ipxe/COPYING.GPLv2 /LICENSE.GPLv2

# Copy the binary to the root as ipxe.efi
COPY --from=builder /ipxe.efi /ipxe.efi
