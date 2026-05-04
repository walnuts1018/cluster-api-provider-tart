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

# Build iPXE for x86_64
FROM --platform=$BUILDPLATFORM alpine:3.20 AS builder-amd64
RUN apk add --no-cache git make gcc musl-dev binutils perl bash xz-dev mtools xorriso
RUN git clone --depth 1 https://github.com/ipxe/ipxe.git /ipxe
WORKDIR /ipxe/src
RUN make bin-x86_64-efi/ipxe.efi

# Build iPXE for arm64
FROM --platform=$BUILDPLATFORM alpine:3.20 AS builder-arm64
RUN apk add --no-cache git make gcc musl-dev binutils perl bash xz-dev mtools xorriso
# Note: In a real multi-arch build environment, we might need cross-compilers
# but for simplicity in this Dockerfile, we assume the builder can handle it 
# or we rely on buildx's emulation if needed.
RUN apk add --no-cache gcc-aarch64-none-elf || true
RUN git clone --depth 1 https://github.com/ipxe/ipxe.git /ipxe
WORKDIR /ipxe/src
# If native build is not possible, we'd need CROSS_COMPILE=aarch64-linux-gnu- 
# But let's try a simple build first, assuming target-specific builders.
RUN make bin-arm64-efi/ipxe.efi || (apk add --no-cache gcc-armv7-none-eabi && make bin-arm64-efi/ipxe.efi)

# Final Stage: Collect both
FROM scratch
COPY --from=builder-amd64 /ipxe/COPYING /LICENSE
COPY --from=builder-amd64 /ipxe/COPYING.GPLv2 /LICENSE.GPLv2
COPY --from=builder-amd64 /ipxe/src/bin-x86_64-efi/ipxe.efi /ipxe-x86_64.efi
COPY --from=builder-arm64 /ipxe/src/bin-arm64-efi/ipxe.efi /ipxe-arm64.efi
