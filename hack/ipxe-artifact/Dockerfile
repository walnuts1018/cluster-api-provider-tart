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

# Common build dependencies for Debian
FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS base
RUN apt-get update && apt-get install -y \
    git \
    make \
    gcc \
    binutils-dev \
    liblzma-dev \
    perl \
    bash \
    mtools \
    xorriso \
    && rm -rf /var/lib/apt/lists/*

# Build iPXE for x86_64
FROM base AS builder-amd64
# Ensure we have the right compiler for x86_64 (native or cross)
RUN if [ "$(uname -m)" != "x86_64" ]; then \
        apt-get update && apt-get install -y gcc-x86-64-linux-gnu && rm -rf /var/lib/apt/lists/*; \
    fi
RUN git clone --depth 1 https://github.com/ipxe/ipxe.git /ipxe
WORKDIR /ipxe/src
RUN if [ "$(uname -m)" != "x86_64" ]; then \
        make CROSS_COMPILE=x86_64-linux-gnu- bin-x86_64-efi/ipxe.efi; \
    else \
        make bin-x86_64-efi/ipxe.efi; \
    fi

# Build iPXE for arm64
FROM base AS builder-arm64
# Ensure we have the right compiler for arm64 (native or cross)
RUN if [ "$(uname -m)" != "aarch64" ]; then \
        apt-get update && apt-get install -y gcc-aarch64-linux-gnu && rm -rf /var/lib/apt/lists/*; \
    fi
RUN git clone --depth 1 https://github.com/ipxe/ipxe.git /ipxe
WORKDIR /ipxe/src
RUN if [ "$(uname -m)" != "aarch64" ]; then \
        make CROSS_COMPILE=aarch64-linux-gnu- bin-arm64-efi/ipxe.efi; \
    else \
        make bin-arm64-efi/ipxe.efi; \
    fi

# Final Stage: Collect both
FROM scratch
COPY --from=builder-amd64 /ipxe/COPYING /LICENSE
COPY --from=builder-amd64 /ipxe/COPYING.GPLv2 /LICENSE.GPLv2
COPY --from=builder-amd64 /ipxe/src/bin-x86_64-efi/ipxe.efi /ipxe-x86_64.efi
COPY --from=builder-arm64 /ipxe/src/bin-arm64-efi/ipxe.efi /ipxe-arm64.efi
